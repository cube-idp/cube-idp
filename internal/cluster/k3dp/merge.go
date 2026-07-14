// Package k3dp implements the k3d ClusterProvider (D4, Phase 3) with the
// same D10 two-layer customization model as kindp: typed fields + a
// provider-native SimpleConfig escape hatch, explicit conflict errors,
// inspectable via `cube-idp config render-cluster`.
package k3dp

import (
	"fmt"
	"maps"
	"os"
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

	"github.com/rafpe/cube-idp/internal/config"
	"github.com/rafpe/cube-idp/internal/diag"
)

// ZotMirror is k3d's CertsD-equivalent (D12): when Host is non-empty,
// RenderConfig adds a registries.yaml mirrors entry for it pointing at the
// node-local zot NodePort. Ensure passes ZotMirror{Host: "registry." + gw.Host};
// cmd/config.go passes the zero value (file-free render, mirror of kindp).
type ZotMirror struct{ Host string }

// RenderConfig performs the D10 two-layer merge and returns the final k3d
// SimpleConfig YAML. It is pure: no docker, no cluster, fully unit-testable.
//
// base   = user providerConfig (file path or inline YAML; a k3d SimpleConfig)
//
//	if set, else an empty v1alpha5.SimpleConfig
//
// inject = gateway port mapping (host gw.Port -> node config.GatewayNodePort
//
//	on server:0), k3sExtraArgs --disable=traefik (server:0) — our gateway
//	pack owns ingress, registry mirrors/insecure as embedded k3s
//	registries.yaml (user spec.cluster.registry entries + the D12 zot
//	mirror when set), typed extraPorts -> ports entries, mounts ->
//	volumes, image rancher/k3s:<kubernetesVersion>-k3s1
//
// conflict = user config maps gw.Port to a different node port, or sets a
//
//	different image than kubernetesVersion implies, or re-enables
//	traefik -> CUBE-1301; unreadable/invalid providerConfig -> CUBE-1302
func RenderConfig(name string, spec config.ClusterSpec, gw config.GatewaySpec, zot ZotMirror) ([]byte, error) {
	cfg, err := loadUserConfig(spec.ProviderConfig)
	if err != nil {
		return nil, err
	}
	cfg.TypeMeta.APIVersion = "k3d.io/v1alpha5"
	cfg.TypeMeta.Kind = "Simple"
	cfg.ObjectMeta.Name = name
	if cfg.Servers == 0 {
		cfg.Servers = 1
	}

	// Node image from kubernetesVersion (conflict on mismatch, D10).
	image := "rancher/k3s:" + spec.KubernetesVersion + "-k3s1"
	if cfg.Image != "" && cfg.Image != image {
		return nil, diag.New(diag.CodeK3dConfigMerge,
			fmt.Sprintf("providerConfig sets image %q but spec.cluster.kubernetesVersion implies %q", cfg.Image, image),
			"remove image from providerConfig or align kubernetesVersion; inspect with `cube-idp config render-cluster`")
	}
	cfg.Image = image

	// Required injection 1: the gateway port mapping (host gw.Port -> node
	// config.GatewayNodePort on the first server).
	gwMapping := fmt.Sprintf("%d:%d", gw.Port, config.GatewayNodePort)
	for _, p := range cfg.Ports {
		host, node, ok := strings.Cut(p.Port, ":")
		if ok && host == fmt.Sprint(gw.Port) && node != fmt.Sprint(config.GatewayNodePort) {
			return nil, diag.New(diag.CodeK3dConfigMerge,
				fmt.Sprintf("providerConfig maps host port %s to %s, but cube-idp requires %s for the gateway", host, node, gwMapping),
				"remove that ports entry or change spec.gateway.port; inspect with `cube-idp config render-cluster`")
		}
	}
	if !hasHostPort(cfg.Ports, gw.Port) {
		cfg.Ports = append(cfg.Ports, v1alpha5.PortWithNodeFilters{
			Port: gwMapping, NodeFilters: []string{"server:0"},
		})
	}

	// Required injection 2: disable k3s's bundled traefik — the gateway pack
	// owns ingress (D3). Conflict if the user explicitly re-enables it.
	disable := v1alpha5.K3sArgWithNodeFilters{Arg: "--disable=traefik", NodeFilters: []string{"server:0"}}
	for _, a := range cfg.Options.K3sOptions.ExtraArgs {
		if strings.Contains(a.Arg, "traefik") && a.Arg != disable.Arg {
			return nil, diag.New(diag.CodeK3dConfigMerge,
				fmt.Sprintf("providerConfig sets k3s arg %q, but cube-idp requires --disable=traefik (the gateway pack provides ingress)", a.Arg),
				"remove the traefik-related k3s arg; the traefik gateway pack replaces the bundled one")
		}
	}
	if !slices.ContainsFunc(cfg.Options.K3sOptions.ExtraArgs, func(a v1alpha5.K3sArgWithNodeFilters) bool { return a.Arg == disable.Arg }) {
		cfg.Options.K3sOptions.ExtraArgs = append(cfg.Options.K3sOptions.ExtraArgs, disable)
	}

	// D10 layer-1 typed fields.
	for _, p := range spec.ExtraPorts {
		if hasHostPort(cfg.Ports, int(p.HostPort)) {
			if int(p.HostPort) == gw.Port {
				return nil, diag.New(diag.CodeK3dConfigMerge,
					fmt.Sprintf("spec.cluster.extraPorts maps hostPort %d which cube-idp reserves for the gateway", p.HostPort),
					"remove that entry from spec.cluster.extraPorts or change spec.gateway.port")
			}
			return nil, diag.New(diag.CodeK3dConfigMerge,
				fmt.Sprintf("host port %d is mapped both in providerConfig and spec.cluster.extraPorts", p.HostPort),
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
			// Two distinct conflicts share this branch — diagnose the one the
			// user actually caused. registries.config is an opaque blob we
			// cannot merge into without parsing, so both are rejected, but on
			// the Ensure path zot.Host is ALWAYS set: blaming
			// spec.cluster.registry there would point at a field the user
			// never touched.
			if len(spec.Registry.Mirrors) > 0 || len(spec.Registry.Insecure) > 0 {
				return nil, diag.New(diag.CodeK3dConfigMerge,
					"registry mirrors are set both in providerConfig (registries.config) and spec.cluster.registry",
					"keep exactly one of the two")
			}
			return nil, diag.New(diag.CodeK3dConfigMerge,
				fmt.Sprintf("providerConfig sets registries.config, but cube-idp must inject a zot registry mirror (%s -> the in-cluster zot NodePort) into the same registries.yaml and cannot merge into an opaque registries.config block", zot.Host),
				"move your mirrors/insecure entries from providerConfig's registries.config into spec.cluster.registry so cube-idp can compose them with the zot mirror")
		}
		cfg.Registries.Config = reg
	}

	return yaml.Marshal(cfg)
}

func loadUserConfig(pc string) (*v1alpha5.SimpleConfig, error) {
	var cfg v1alpha5.SimpleConfig
	if pc == "" {
		return &cfg, nil
	}
	raw := []byte(pc)
	if !strings.Contains(pc, "\n") { // single line -> file path (same rule as kindp)
		b, err := os.ReadFile(pc)
		if err != nil {
			return nil, diag.Wrap(err, diag.CodeK3dConfigInvalid, fmt.Sprintf("cannot read providerConfig file %s", pc),
				"set spec.cluster.providerConfig to a readable k3d SimpleConfig file or an inline YAML document")
		}
		raw = b
	}
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return nil, diag.Wrap(err, diag.CodeK3dConfigInvalid, "providerConfig is not a valid k3d SimpleConfig document",
			"see https://k3d.io/stable/usage/configfile/")
	}
	return &cfg, nil
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
// insecure TLS skip + the D12 zot mirror when zot.Host is set: an entry
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
