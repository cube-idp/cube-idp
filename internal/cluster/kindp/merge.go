// Package kindp implements the kind ClusterProvider (spec §4.1) and the
// cluster customization ladder (spec 2026-07-18-cluster-forprovider-design.md
// §4): providerConfigRef (fetched base) + forProvider (inline overrides,
// RFC 7386 merge-patched on top) composed generically by internal/cluster/
// compose, strict-decoded into v1alpha4.Cluster, then typed sugar
// (extraPorts/mounts/registry) and cube-idp's core injections applied here.
package kindp

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"

	"gopkg.in/yaml.v3"
	v1alpha4 "sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
	sigyaml "sigs.k8s.io/yaml"

	"github.com/cube-idp/cube-idp/internal/cluster/compose"
	"github.com/cube-idp/cube-idp/internal/config"
	"github.com/cube-idp/cube-idp/internal/diag"
	"github.com/cube-idp/cube-idp/internal/pack"
)

// gatewayContainerPort is the kind node port the gateway hostPort maps to.
// It pins to the traefik starter pack's fixed websecure NodePort
// (config.GatewayNodePort, chart values ports.websecure.nodePort /
// service.spec.type: NodePort) rather than 443: Phase 2 terminates TLS at
// Traefik with a cube-idp CA-issued cert (spec D6/D12, internal/up/tls.go),
// and a NodePort Service is simpler than wiring a hostPort/LoadBalancer
// controller into a kind node. See packs/traefik/README.md for the full
// host->node->pod chain. Shared with k3dp via config.GatewayNodePort (not
// internal/cluster: that package's factory imports kindp/k3dp, so importing
// it back here would cycle).
const gatewayContainerPort = config.GatewayNodePort

// CertsD requests a containerd certs.d bind mount on every kind node so
// image refs on Host resolve through the hosts.toml/ca.crt staged in HostDir
// (internal/trust.WriteCertsD) rather than through localtest.me, which on a
// kind node resolves to the node itself (D6). Zero value = no injection.
type CertsD struct{ Host, HostDir string }

// RenderConfig composes the customization ladder (spec 2026-07-18 §4) and
// returns the final kind config YAML plus core-override warnings. Pure
// except the providerConfigRef fetch: no docker, no cluster.
//
// layers 1-2 = compose.Compose(providerConfigRef, forProvider), strict-
//
//	decoded into v1alpha4.Cluster (unknown field -> CUBE-1202)
//
// layer 3 = typed extraPorts/mounts/registry — user-vs-user conflicts stay
//
//	hard CUBE-1201 errors
//
// layer 4 = core injections (name/kind/apiVersion, gateway ports, node
//
//	image, certs.d) — core wins, CUBE-1206 warning (decision 1)
func RenderConfig(ctx context.Context, name string, spec config.ClusterSpec, gw config.GatewaySpec, certsd CertsD) ([]byte, []diag.Finding, error) {
	var warns []diag.Finding
	warn := func(msg string) {
		warns = append(warns, diag.Finding{Code: diag.CodeKindCoreOverride, Severity: diag.SeverityWarning,
			Message: msg, Remediation: "inspect the final config with `cube-idp config render-cluster`"})
	}
	cacheDir, err := pack.DefaultCacheDir()
	if err != nil {
		return nil, nil, err
	}
	merged, err := compose.Compose(ctx, spec.ProviderConfigRef, spec.ForProvider, cacheDir)
	if err != nil {
		return nil, nil, err
	}
	cfg := &v1alpha4.Cluster{}
	if len(merged) > 0 {
		j, err := json.Marshal(merged)
		if err != nil {
			return nil, nil, diag.Wrap(err, diag.CodeKindConfigInvalid, "cannot encode composed provider config", "report this as a bug")
		}
		if err := sigyaml.UnmarshalStrict(j, cfg); err != nil {
			return nil, nil, diag.Wrap(err, diag.CodeKindConfigInvalid,
				"providerConfigRef/forProvider is not a valid kind Cluster document",
				"unknown or mistyped field — see https://kind.sigs.k8s.io/docs/user/configuration/ and docs/superpowers/specs/2026-07-18-kind-config-reference.md")
		}
	}
	cfg.Kind = "Cluster"
	cfg.APIVersion = "kind.x-k8s.io/v1alpha4"
	cfg.Name = name
	if len(cfg.Nodes) == 0 {
		cfg.Nodes = []v1alpha4.Node{{Role: v1alpha4.ControlPlaneRole}}
	}
	cp := controlPlane(cfg.Nodes)
	if cp == nil {
		return nil, nil, diag.New(diag.CodeKindConfigInvalid, "providerConfigRef/forProvider declares no control-plane node",
			"add a node with role: control-plane to providerConfigRef/forProvider; see https://kind.sigs.k8s.io/docs/user/configuration/#nodes")
	}

	image := "kindest/node:" + spec.KubernetesVersion
	if cp.Image != "" && cp.Image != image {
		warn(fmt.Sprintf("overriding node image %q with %q — spec.cluster.kubernetesVersion wins over providerConfigRef/forProvider", cp.Image, image))
	}
	cp.Image = image

	// Required injection: gateway port (spec D10 "injects only what it
	// requires"). Guarded (U2, pre-created for S3): a zero gw.Port means "no
	// gateway on this cluster" — spoke clusters render with a zero
	// GatewaySpec — and injects no mapping at all.
	if gw.Port > 0 {
		for i := range cp.ExtraPortMappings {
			pm := &cp.ExtraPortMappings[i]
			if pm.HostPort == int32(gw.Port) && pm.ContainerPort != gatewayContainerPort {
				warn(fmt.Sprintf("rewriting extraPortMapping %d -> %d to %d -> %d — the gateway requires this containerPort",
					gw.Port, pm.ContainerPort, gw.Port, gatewayContainerPort))
				pm.ContainerPort = gatewayContainerPort
			}
		}
		if !hasHostPort(cp.ExtraPortMappings, int32(gw.Port)) {
			cp.ExtraPortMappings = append(cp.ExtraPortMappings, v1alpha4.PortMapping{
				ContainerPort: gatewayContainerPort, HostPort: int32(gw.Port), Protocol: v1alpha4.PortMappingProtocolTCP,
			})
		}
		// U2 opt-in HTTP twin (decision 3): host gw.HTTPPort -> the
		// plain-HTTP NodePort both gateway packs pin
		// (config.GatewayHTTPNodePort). Absent = no mapping, byte-identical
		// output to before.
		if gw.HTTPPort > 0 {
			for i := range cp.ExtraPortMappings {
				pm := &cp.ExtraPortMappings[i]
				if pm.HostPort == int32(gw.HTTPPort) && pm.ContainerPort != config.GatewayHTTPNodePort {
					warn(fmt.Sprintf("rewriting extraPortMapping %d -> %d to %d -> %d — the gateway requires this containerPort for its HTTP listener",
						gw.HTTPPort, pm.ContainerPort, gw.HTTPPort, config.GatewayHTTPNodePort))
					pm.ContainerPort = config.GatewayHTTPNodePort
				}
			}
			if !hasHostPort(cp.ExtraPortMappings, int32(gw.HTTPPort)) {
				cp.ExtraPortMappings = append(cp.ExtraPortMappings, v1alpha4.PortMapping{
					ContainerPort: config.GatewayHTTPNodePort, HostPort: int32(gw.HTTPPort), Protocol: v1alpha4.PortMappingProtocolTCP,
				})
			}
		}
	}

	// D10 layer-1 typed fields.
	for _, p := range spec.ExtraPorts {
		if hasHostPort(cp.ExtraPortMappings, p.HostPort) {
			if p.HostPort == int32(gw.Port) {
				return nil, nil, diag.New(diag.CodeKindConfigMerge,
					fmt.Sprintf("spec.cluster.extraPorts maps hostPort %d which cube-idp reserves for the gateway", p.HostPort),
					"remove that entry from spec.cluster.extraPorts or change spec.gateway.port")
			}
			if gw.HTTPPort > 0 && p.HostPort == int32(gw.HTTPPort) {
				return nil, nil, diag.New(diag.CodeKindConfigMerge,
					fmt.Sprintf("spec.cluster.extraPorts maps hostPort %d which cube-idp reserves for the gateway's HTTP listener", p.HostPort),
					"remove that entry from spec.cluster.extraPorts or change spec.gateway.httpPort")
			}
			return nil, nil, diag.New(diag.CodeKindConfigMerge,
				fmt.Sprintf("hostPort %d is mapped both in providerConfigRef/forProvider and spec.cluster.extraPorts", p.HostPort),
				"keep exactly one of the two mappings")
		}
		cp.ExtraPortMappings = append(cp.ExtraPortMappings, v1alpha4.PortMapping{
			ContainerPort: p.NodePort, HostPort: p.HostPort, Protocol: v1alpha4.PortMappingProtocolTCP,
		})
	}
	for _, m := range spec.Mounts {
		cp.ExtraMounts = append(cp.ExtraMounts, v1alpha4.Mount{HostPath: m.HostPath, ContainerPath: m.NodePath})
	}
	cfg.ContainerdConfigPatches = append(cfg.ContainerdConfigPatches, containerdPatches(spec.Registry)...)

	// D6 canonical hostname: containerd certs.d injection (Task 10).
	if certsd.Host != "" {
		cfg.ContainerdConfigPatches = append(cfg.ContainerdConfigPatches,
			"[plugins.\"io.containerd.grpc.v1.cri\".registry]\n  config_path = \"/etc/containerd/certs.d\"")
		cp.ExtraMounts = append(cp.ExtraMounts, v1alpha4.Mount{
			HostPath: certsd.HostDir, ContainerPath: "/etc/containerd/certs.d/" + certsd.Host})
	}

	out, err := yaml.Marshal(cfg)
	return out, warns, err
}

// controlPlane returns the first node with the control-plane role, or nil if
// the node list declares none. All cube-idp injections (gateway port, node
// image, typed extraPorts, mounts) target this node — never Nodes[0] blindly,
// since a user providerConfigRef/forProvider may list workers first.
func controlPlane(nodes []v1alpha4.Node) *v1alpha4.Node {
	for i := range nodes {
		if nodes[i].Role == v1alpha4.ControlPlaneRole {
			return &nodes[i]
		}
	}
	return nil
}

func hasHostPort(pms []v1alpha4.PortMapping, host int32) bool {
	for _, pm := range pms {
		if pm.HostPort == host {
			return true
		}
	}
	return false
}

// containerdPatches renders registry mirror/insecure config as containerd
// config patches. Mirror hostnames are sorted before emitting so output is
// deterministic (map iteration order is otherwise random).
func containerdPatches(r config.RegistrySpec) []string {
	var out []string
	for _, host := range slices.Sorted(maps.Keys(r.Mirrors)) {
		out = append(out, fmt.Sprintf(
			"[plugins.\"io.containerd.grpc.v1.cri\".registry.mirrors.%q]\n  endpoint = [%q]", host, r.Mirrors[host]))
	}
	for _, host := range r.Insecure {
		out = append(out, fmt.Sprintf(
			"[plugins.\"io.containerd.grpc.v1.cri\".registry.configs.%q.tls]\n  insecure_skip_verify = true", host))
	}
	return out
}
