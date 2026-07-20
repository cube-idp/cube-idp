// Package k3dp implements the k3d ClusterProvider and the
// cluster customization ladder (spec 2026-07-18-cluster-forprovider-design.md
// §4): providerConfigRef (fetched base) + forProvider (inline overrides,
// RFC 7386 merge-patched on top) composed generically by internal/cluster/
// compose, strict-decoded into v1alpha5.SimpleConfig, then typed sugar
// (extraPorts/mounts/registry) and cube-idp's core injections applied here.
package k3dp

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

	v1alpha5 "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	// v1alpha5.SimpleConfig carries only `mapstructure`/`json` struct tags —
	// unlike kind's v1alpha4.Cluster (which has `yaml` tags and lets kindp use
	// gopkg.in/yaml.v3 directly), gopkg.in/yaml.v3 would silently fall back to
	// lowercased-no-tag field names here (e.g. "apiversion", not "apiVersion")
	// and desync from the real k3d config schema. sigs.k8s.io/yaml marshals
	// through encoding/json first, so it honors the `json` tags and produces
	// the same field names k3d's own config loader (viper+mapstructure) reads
	// and writes. Already a repo dependency (cmd/init.go, config/load_test.go).
	"sigs.k8s.io/yaml"

	"github.com/cube-idp/cube-idp/internal/cluster/compose"
	"github.com/cube-idp/cube-idp/internal/config"
	"github.com/cube-idp/cube-idp/internal/diag"
	"github.com/cube-idp/cube-idp/internal/pack"
)

// ZotMirror is k3d's CertsD-equivalent — the local-CA/registry-trust wiring
// that must exist before the cluster is created. When Host is non-empty,
// RenderConfig adds a registries.yaml mirrors entry for it pointing at the
// node-local zot NodePort. Ensure passes ZotMirror{Host: "registry." + gw.Host};
// cmd/config.go passes the zero value (file-free render, mirror of kindp).
type ZotMirror struct{ Host string }

// RenderConfig composes the customization ladder (spec 2026-07-18 §4) and
// returns the final k3d config YAML plus core-override warnings. Pure
// except the providerConfigRef fetch: no docker, no cluster.
//
// layers 1-2 = compose.Compose(providerConfigRef, forProvider), strict-
//
//	decoded into v1alpha5.SimpleConfig (unknown field -> CUBE-1302)
//
// layer 3 = typed extraPorts/mounts/registry — user-vs-user conflicts stay
//
//	hard CUBE-1301 errors
//
// layer 4 = core injections (name/kind/apiVersion, gateway ports, node
//
//	image, --disable=traefik, zot mirror) — core wins, CUBE-1306 warning
//	(user-supplied values are overridden, never silently)
func RenderConfig(ctx context.Context, name string, spec config.ClusterSpec, gw config.GatewaySpec, zot ZotMirror) ([]byte, []diag.Finding, error) {
	var warns []diag.Finding
	warn := func(msg string) {
		warns = append(warns, diag.Finding{Code: diag.CodeK3dCoreOverride, Severity: diag.SeverityWarning,
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
	cfg := &v1alpha5.SimpleConfig{}
	if len(merged) > 0 {
		j, err := json.Marshal(merged)
		if err != nil {
			return nil, nil, diag.Wrap(err, diag.CodeK3dConfigInvalid, "cannot encode composed provider config", "report this as a bug")
		}
		if err := yaml.UnmarshalStrict(j, cfg); err != nil { // yaml = sigs.k8s.io/yaml, already this package's import
			return nil, nil, diag.Wrap(err, diag.CodeK3dConfigInvalid,
				"providerConfigRef/forProvider is not a valid k3d SimpleConfig document",
				"unknown or mistyped field — see https://k3d.io/stable/usage/configfile/")
		}
	}
	cfg.TypeMeta.APIVersion = "k3d.io/v1alpha5"
	cfg.TypeMeta.Kind = "Simple"
	cfg.ObjectMeta.Name = name
	if cfg.Servers == 0 {
		cfg.Servers = 1
	}

	// Node image from kubernetesVersion (core wins, CUBE-1306 warning).
	image := "rancher/k3s:" + spec.KubernetesVersion + "-k3s1"
	if cfg.Image != "" && cfg.Image != image {
		warn(fmt.Sprintf("overriding image %q with %q — spec.cluster.kubernetesVersion wins over providerConfigRef/forProvider", cfg.Image, image))
	}
	cfg.Image = image

	// Required injection 1: the gateway port mapping (host gw.Port -> node
	// config.GatewayNodePort on the first server). Guarded (U2, pre-created
	// for S3): a zero gw.Port means "no gateway on this cluster" and injects
	// no mapping at all.
	if gw.Port > 0 {
		gwMapping := fmt.Sprintf("%d:%d", gw.Port, config.GatewayNodePort)
		for i := range cfg.Ports {
			p := &cfg.Ports[i]
			host, node, ok := strings.Cut(p.Port, ":")
			if ok && host == fmt.Sprint(gw.Port) && node != fmt.Sprint(config.GatewayNodePort) {
				warn(fmt.Sprintf("rewriting ports entry %s -> %s — the gateway requires this mapping", p.Port, gwMapping))
				p.Port = gwMapping
			}
		}
		if !hasHostPort(cfg.Ports, gw.Port) {
			cfg.Ports = append(cfg.Ports, v1alpha5.PortWithNodeFilters{
				Port: gwMapping, NodeFilters: []string{"server:0"},
			})
		}
		// Opt-in plain-HTTP twin of the gateway mapping: host gw.HTTPPort -> the
		// plain-HTTP NodePort both gateway packs pin
		// (config.GatewayHTTPNodePort), same host:node syntax. Absent = no
		// mapping, byte-identical output to before.
		if gw.HTTPPort > 0 {
			httpMapping := fmt.Sprintf("%d:%d", gw.HTTPPort, config.GatewayHTTPNodePort)
			for i := range cfg.Ports {
				p := &cfg.Ports[i]
				host, node, ok := strings.Cut(p.Port, ":")
				if ok && host == fmt.Sprint(gw.HTTPPort) && node != fmt.Sprint(config.GatewayHTTPNodePort) {
					warn(fmt.Sprintf("rewriting ports entry %s -> %s — the gateway requires this mapping for its HTTP listener", p.Port, httpMapping))
					p.Port = httpMapping
				}
			}
			if !hasHostPort(cfg.Ports, gw.HTTPPort) {
				cfg.Ports = append(cfg.Ports, v1alpha5.PortWithNodeFilters{
					Port: httpMapping, NodeFilters: []string{"server:0"},
				})
			}
		}
	}

	// Required injection 2: disable k3s's bundled traefik — the gateway pack
	// owns ingress. Core wins on an explicit re-enable, CUBE-1306
	// warning quoting the discarded arg.
	disable := v1alpha5.K3sArgWithNodeFilters{Arg: "--disable=traefik", NodeFilters: []string{"server:0"}}
	for i := range cfg.Options.K3sOptions.ExtraArgs {
		a := &cfg.Options.K3sOptions.ExtraArgs[i]
		if strings.Contains(a.Arg, "traefik") && a.Arg != disable.Arg {
			warn(fmt.Sprintf("replacing k3s arg %q with %q — the gateway pack provides ingress", a.Arg, disable.Arg))
			a.Arg = disable.Arg
		}
	}
	if !slices.ContainsFunc(cfg.Options.K3sOptions.ExtraArgs, func(a v1alpha5.K3sArgWithNodeFilters) bool { return a.Arg == disable.Arg }) {
		cfg.Options.K3sOptions.ExtraArgs = append(cfg.Options.K3sOptions.ExtraArgs, disable)
	}

	// Typed `spec.cluster` sugar (extra ports, mounts, registry mirrors, node
	// image) — the layer that collides hard on conflict; see ADR 0011.
	for _, p := range spec.ExtraPorts {
		if hasHostPort(cfg.Ports, int(p.HostPort)) {
			if int(p.HostPort) == gw.Port {
				return nil, nil, diag.New(diag.CodeK3dConfigMerge,
					fmt.Sprintf("spec.cluster.extraPorts maps hostPort %d which cube-idp reserves for the gateway", p.HostPort),
					"remove that entry from spec.cluster.extraPorts or change spec.gateway.port")
			}
			if gw.HTTPPort > 0 && int(p.HostPort) == gw.HTTPPort {
				return nil, nil, diag.New(diag.CodeK3dConfigMerge,
					fmt.Sprintf("spec.cluster.extraPorts maps hostPort %d which cube-idp reserves for the gateway's HTTP listener", p.HostPort),
					"remove that entry from spec.cluster.extraPorts or change spec.gateway.httpPort")
			}
			return nil, nil, diag.New(diag.CodeK3dConfigMerge,
				fmt.Sprintf("hostPort %d is mapped both in providerConfigRef/forProvider and spec.cluster.extraPorts", p.HostPort),
				"keep exactly one of the two mappings")
		}
		cfg.Ports = append(cfg.Ports, v1alpha5.PortWithNodeFilters{
			Port: fmt.Sprintf("%d:%d", p.HostPort, p.NodePort), NodeFilters: []string{"server:0"},
		})
	}
	for _, m := range spec.Mounts {
		cfg.Volumes = append(cfg.Volumes, v1alpha5.VolumeWithNodeFilters{
			Volume: m.HostPath + ":" + m.NodePath, NodeFilters: []string{"server:0"},
		})
	}
	if reg := registriesYAML(spec.Registry, zot); reg != "" {
		if cfg.Registries.Config != "" {
			// Two distinct cases share this branch. A user-vs-user conflict
			// (spec.cluster.registry also set) stays a hard CUBE-1301 error —
			// cube-idp cannot silently pick a winner between two things the
			// user wrote. A user-vs-core conflict (only the Ensure-path zot
			// mirror needs the slot, spec.cluster.registry untouched) is
			// the same warn-and-win rule: registries.config is an opaque blob
			// that cannot be merged into without parsing, so the user's block
			// is discarded in favor of cube-idp's, with a warning naming what
			// was dropped.
			if len(spec.Registry.Mirrors) > 0 || len(spec.Registry.Insecure) > 0 {
				return nil, nil, diag.New(diag.CodeK3dConfigMerge,
					"registry mirrors are set both in providerConfigRef/forProvider (registries.config) and spec.cluster.registry",
					"keep exactly one of the two")
			}
			warn(fmt.Sprintf("discarding providerConfigRef/forProvider registries.config %q — cube-idp must inject a zot registry mirror (%s -> the in-cluster zot NodePort) into the same registries.yaml and cannot merge into an opaque registries.config block",
				cfg.Registries.Config, zot.Host))
		}
		cfg.Registries.Config = reg
	}

	out, err := yaml.Marshal(cfg)
	return out, warns, err
}

func hasHostPort(ports []v1alpha5.PortWithNodeFilters, host int) bool {
	for _, p := range ports {
		if h, _, ok := strings.Cut(p.Port, ":"); ok && h == fmt.Sprint(host) {
			return true
		}
	}
	return false
}

// registriesYAML renders the k3s registries.yaml document (mirrors +
// insecure TLS skip + the in-cluster zot mirror when zot.Host is set: an entry
// zot.Host -> endpoint http://localhost:30500, i.e. registry.NodePort —
// plain HTTP on the node-local port, same as kindp's WriteCertsD wiring),
// sorted for golden determinism. The k3s registries.yaml schema
// (mirrors.<host>.endpoint, configs.<host>.tls.insecure_skip_verify) is
// github.com/rancher/wharfie/pkg/registries.Registry, a k3d transitive
// dependency; this hand-rolled shape avoids importing it just for this.
func registriesYAML(r config.RegistrySpec, zot ZotMirror) string {
	if len(r.Mirrors) == 0 && len(r.Insecure) == 0 && zot.Host == "" {
		return ""
	}
	type endpointCfg struct {
		Endpoint []string `json:"endpoint"`
	}
	type tlsCfg struct {
		TLS map[string]bool `json:"tls,omitempty"`
	}
	doc := struct {
		Mirrors map[string]endpointCfg `json:"mirrors,omitempty"`
		Configs map[string]tlsCfg      `json:"configs,omitempty"`
	}{}
	if len(r.Mirrors) > 0 || zot.Host != "" {
		doc.Mirrors = map[string]endpointCfg{}
		for _, host := range slices.Sorted(maps.Keys(r.Mirrors)) {
			doc.Mirrors[host] = endpointCfg{Endpoint: []string{r.Mirrors[host]}}
		}
		if zot.Host != "" {
			doc.Mirrors[zot.Host] = endpointCfg{Endpoint: []string{"http://localhost:30500"}}
		}
	}
	if len(r.Insecure) > 0 {
		doc.Configs = map[string]tlsCfg{}
		for _, host := range r.Insecure {
			doc.Configs[host] = tlsCfg{TLS: map[string]bool{"insecure_skip_verify": true}}
		}
	}
	out, _ := yaml.Marshal(doc)
	return string(out)
}
