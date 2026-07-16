// Package kindp implements the kind ClusterProvider (spec §4.1) and the D10
// two-layer provider-config merge: typed cube.yaml fields (layer 1) and an
// optional user-supplied kind config (layer 2, file path or inline YAML).
package kindp

import (
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
	v1alpha4 "sigs.k8s.io/kind/pkg/apis/config/v1alpha4"

	"github.com/cube-idp/cube-idp/internal/config"
	"github.com/cube-idp/cube-idp/internal/diag"
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

// RenderConfig performs the D10 two-layer merge and returns the final kind
// config YAML. It is pure: no docker, no cluster, fully unit-testable.
//
// base   = user providerConfig (file path or inline YAML) if set, else an
//
//	empty v1alpha4.Cluster
//
// inject = gateway extraPortMapping (hostPort=gw.Port -> containerPort 30443),
//
//	registry mirrors/insecure as containerdConfigPatches, typed
//	extraPorts + mounts on the control-plane node, node image from
//	kubernetesVersion (kindest/node:<version>)
//
// conflict = user config already maps gw.Port to a different containerPort,
//
//	or sets a different node image than kubernetesVersion -> CUBE-1201
func RenderConfig(name string, spec config.ClusterSpec, gw config.GatewaySpec, certsd CertsD) ([]byte, error) {
	cfg, err := loadUserConfig(spec.ProviderConfig)
	if err != nil {
		return nil, err
	}
	cfg.Kind = "Cluster"
	cfg.APIVersion = "kind.x-k8s.io/v1alpha4"
	cfg.Name = name
	if len(cfg.Nodes) == 0 {
		cfg.Nodes = []v1alpha4.Node{{Role: v1alpha4.ControlPlaneRole}}
	}
	cp := controlPlane(cfg.Nodes)
	if cp == nil {
		return nil, diag.New(diag.CodeKindConfigInvalid, "providerConfig declares no control-plane node",
			"add a node with role: control-plane to providerConfig; see https://kind.sigs.k8s.io/docs/user/configuration/#nodes")
	}

	image := "kindest/node:" + spec.KubernetesVersion
	if cp.Image != "" && cp.Image != image {
		return nil, diag.New(diag.CodeKindConfigMerge,
			fmt.Sprintf("providerConfig sets node image %q but spec.cluster.kubernetesVersion implies %q", cp.Image, image),
			"remove the image from providerConfig or align kubernetesVersion; inspect with `cube-idp config render-cluster`")
	}
	cp.Image = image

	// Required injection: gateway port (spec D10 "injects only what it requires").
	for _, pm := range cp.ExtraPortMappings {
		if pm.HostPort == int32(gw.Port) && pm.ContainerPort != gatewayContainerPort {
			return nil, diag.New(diag.CodeKindConfigMerge,
				fmt.Sprintf("providerConfig maps hostPort %d to containerPort %d, but cube-idp requires %d -> %d for the gateway",
					gw.Port, pm.ContainerPort, gw.Port, gatewayContainerPort),
				"remove that extraPortMapping or change spec.gateway.port; inspect with `cube-idp config render-cluster`")
		}
	}
	if !hasHostPort(cp.ExtraPortMappings, int32(gw.Port)) {
		cp.ExtraPortMappings = append(cp.ExtraPortMappings, v1alpha4.PortMapping{
			ContainerPort: gatewayContainerPort, HostPort: int32(gw.Port), Protocol: v1alpha4.PortMappingProtocolTCP,
		})
	}

	// D10 layer-1 typed fields.
	for _, p := range spec.ExtraPorts {
		if hasHostPort(cp.ExtraPortMappings, p.HostPort) {
			if p.HostPort == int32(gw.Port) {
				return nil, diag.New(diag.CodeKindConfigMerge,
					fmt.Sprintf("spec.cluster.extraPorts maps hostPort %d which cube-idp reserves for the gateway", p.HostPort),
					"remove that entry from spec.cluster.extraPorts or change spec.gateway.port")
			}
			return nil, diag.New(diag.CodeKindConfigMerge,
				fmt.Sprintf("hostPort %d is mapped both in providerConfig and spec.cluster.extraPorts", p.HostPort),
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

	return yaml.Marshal(cfg)
}

func loadUserConfig(pc string) (*v1alpha4.Cluster, error) {
	var cfg v1alpha4.Cluster
	if pc == "" {
		return &cfg, nil
	}
	raw := []byte(pc)
	if !strings.Contains(pc, "\n") { // single line -> treat as file path
		b, err := os.ReadFile(pc)
		if err != nil {
			return nil, diag.Wrap(err, diag.CodeKindConfigInvalid, fmt.Sprintf("cannot read providerConfig file %s", pc),
				"set spec.cluster.providerConfig to a readable kind config file or an inline YAML document")
		}
		raw = b
	}
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return nil, diag.Wrap(err, diag.CodeKindConfigInvalid, "providerConfig is not a valid kind Cluster document",
			"see https://kind.sigs.k8s.io/docs/user/configuration/")
	}
	return &cfg, nil
}

// controlPlane returns the first node with the control-plane role, or nil if
// the node list declares none. All cube-idp injections (gateway port, node
// image, typed extraPorts, mounts) target this node — never Nodes[0] blindly,
// since a user providerConfig may list workers first.
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
