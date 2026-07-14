package k3dp

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	k3dclient "github.com/k3d-io/k3d/v5/pkg/client"
	k3dconfig "github.com/k3d-io/k3d/v5/pkg/config"
	v1alpha5 "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	"github.com/k3d-io/k3d/v5/pkg/runtimes"
	k3dtypes "github.com/k3d-io/k3d/v5/pkg/types"
	k3dutil "github.com/k3d-io/k3d/v5/pkg/util"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/yaml"

	"github.com/rafpe/cube-idp/internal/bundle"
	"github.com/rafpe/cube-idp/internal/config"
	"github.com/rafpe/cube-idp/internal/diag"
	"github.com/rafpe/cube-idp/internal/kube"
)

// K3d implements cluster.Provider for local k3d (k3s-in-docker) clusters
// (spec §4.1, D4). It is a thin wrapper around github.com/k3d-io/k3d/v5: all
// config assembly happens in RenderConfig (merge.go), which stays pure and
// cluster-free — the same split kindp uses.
//
// Note: this file does not import internal/cluster to add a compile-time
// `var _ cluster.Provider = (*K3d)(nil)` assertion. internal/cluster's
// factory (provider.go) imports this package to construct k3dp.New in its
// New() switch, so the reverse import would be a cycle (kindp has the same
// constraint and lacks the assertion for the same reason). K3d's shape is
// still checked at compile time: internal/cluster/provider.go's `case "k3d":
// return k3dp.New(gw), nil` only compiles because *K3d satisfies
// cluster.Provider (New's return type there is cluster.Provider).
type K3d struct{ gw config.GatewaySpec }

// New returns a K3d provider bound to the given gateway spec.
func New(gw config.GatewaySpec) *K3d { return &K3d{gw: gw} }

// Ensure creates the named k3d cluster if it doesn't already exist, then
// returns a connection to it.
func (k *K3d) Ensure(ctx context.Context, name string, spec config.ClusterSpec) (*kube.Conn, error) {
	exists, err := k.Exists(ctx, name)
	if err != nil {
		return nil, err
	}
	if !exists {
		cfgYAML, err := RenderConfig(name, spec, k.gw, ZotMirror{Host: "registry." + k.gw.Host})
		if err != nil {
			return nil, err
		}
		var simpleCfg v1alpha5.SimpleConfig
		if err := yaml.Unmarshal(cfgYAML, &simpleCfg); err != nil {
			return nil, diag.Wrap(err, diag.CodeK3dConfigInvalid, "rendered k3d SimpleConfig failed to parse",
				"this is a cube-idp bug; run `cube-idp config render-cluster` and inspect the output")
		}
		// Pin the API-server host port BEFORE transform. k3d's own CLI does
		// this in cmd/cluster/clusterCreate.go ("Set to random port if port
		// is empty string"), but the reusable pkg/config transform we call
		// does NOT — it leaves ExposeAPI.HostPort as "". An empty host port is
		// baked into the server node's k3d.server.api.port label at creation,
		// and KubeconfigGet reads that label verbatim: the resulting REST
		// config is https://0.0.0.0 with NO port (dial 0.0.0.0:443 → refused),
		// which is exactly the CUBE-2003 failure the Phase 3 e2e caught.
		if err := ensureExposedAPIPort(&simpleCfg); err != nil {
			return nil, err
		}
		clusterConfig, err := k3dconfig.TransformSimpleToClusterConfig(ctx, runtimes.SelectedRuntime, simpleCfg, "")
		if err != nil {
			return nil, diag.Wrap(err, diag.CodeK3dConfigInvalid, "k3d SimpleConfig could not be transformed into a cluster config",
				"inspect `cube-idp config render-cluster` output; see https://k3d.io/stable/usage/configfile/")
		}
		if err := k3dconfig.ValidateClusterConfig(ctx, runtimes.SelectedRuntime, *clusterConfig); err != nil {
			return nil, diag.Wrap(err, diag.CodeK3dConfigInvalid, "k3d cluster config failed validation",
				"inspect `cube-idp config render-cluster` output; see https://k3d.io/stable/usage/configfile/")
		}
		if err := k3dclient.ClusterRun(ctx, runtimes.SelectedRuntime, clusterConfig); err != nil {
			return nil, diag.Wrap(err, diag.CodeK3dCreateFailed, "k3d cluster creation failed",
				"check that the container runtime is running and has free resources; `cube-idp doctor` will preflight this")
		}
	}
	return k.connect(ctx, name)
}

// ensureExposedAPIPort pins a concrete host port for the exposed Kubernetes
// API when the user left it unset. k3d's CLI does this itself
// (cmd/cluster/clusterCreate.go: "Set to random port if port is empty
// string") but the pkg/config transform we drive directly does not — and an
// empty ExposeAPI.HostPort silently produces a portless https://0.0.0.0
// kubeconfig (see the call site in Ensure). Assigning a free port here makes
// the serverlb publish that exact port AND records it in the server node's
// k3d.server.api.port label, so the kubeconfig KubeconfigGet serializes
// carries the real, reachable mapped port. A user-supplied port is preserved.
func ensureExposedAPIPort(cfg *v1alpha5.SimpleConfig) error {
	if cfg.ExposeAPI.HostPort != "" {
		return nil
	}
	port, err := k3dutil.GetFreePort()
	if err != nil || port == 0 {
		return diag.Wrap(err, diag.CodeK3dCreateFailed, "could not allocate a free host port for the k3d API server",
			"retry; if it persists, another process may be exhausting local ports")
	}
	cfg.ExposeAPI.HostPort = strconv.Itoa(port)
	return nil
}

// connect fetches the kubeconfig for the named k3d cluster and builds a
// kube.Conn from it.
func (k *K3d) connect(ctx context.Context, name string) (*kube.Conn, error) {
	kcAPI, err := k3dclient.KubeconfigGet(ctx, runtimes.SelectedRuntime, &k3dtypes.Cluster{Name: name})
	if err != nil {
		return nil, diag.Wrap(err, diag.CodeK3dKubeconfigGet, "cannot get kubeconfig from k3d", "retry; if it persists, `cube-idp down` and `up` again")
	}
	kc, err := clientcmd.Write(*kcAPI)
	if err != nil {
		return nil, diag.Wrap(err, diag.CodeK3dKubeconfigGet, "k3d kubeconfig is invalid", "delete the cluster with `cube-idp down` and retry")
	}
	restCfg, err := clientcmd.RESTConfigFromKubeConfig(kc)
	if err != nil {
		return nil, diag.Wrap(err, diag.CodeK3dKubeconfigGet, "k3d kubeconfig is invalid", "delete the cluster with `cube-idp down` and retry")
	}
	return &kube.Conn{Kubeconfig: kc, Context: "k3d-" + name, REST: restCfg}, nil
}

// LoadImages implements cluster.ImageLoader: it imports every bundled
// per-image OCI-layout tar into the named k3d cluster's nodes in a single
// k3d call, so `up --bundle` pods start without any registry pull. Tar paths
// are ordered by image ref (bundle.SortedImageLoads) for a deterministic,
// reproducible import; ImageImportIntoClusterMulti loads a tar archive when
// the "image" it is given is a filesystem path. On failure the whole load
// wraps as CUBE-7002 (k3d imports all tars in one call, so the failing image
// is not individually identifiable here — the tar list is named instead).
func (k *K3d) LoadImages(ctx context.Context, name string, imageTars map[string]string) error {
	loads := bundle.SortedImageLoads(imageTars)
	if len(loads) == 0 {
		return nil
	}
	tarPaths := make([]string, len(loads))
	refs := make([]string, len(loads))
	for i, l := range loads {
		tarPaths[i] = l.Tar
		refs[i] = l.Ref
	}
	if err := k3dclient.ImageImportIntoClusterMulti(ctx, runtimes.SelectedRuntime, tarPaths,
		&k3dtypes.Cluster{Name: name}, k3dtypes.ImageImportOpts{}); err != nil {
		return diag.Wrap(err, diag.CodeVendorPullFail,
			fmt.Sprintf("cannot load images into cluster nodes (%s)", strings.Join(refs, ", ")),
			"verify the bundle with `cube-idp vendor` on a connected machine and retry")
	}
	return nil
}

// Exists reports whether a k3d cluster with the given name exists.
func (k *K3d) Exists(ctx context.Context, name string) (bool, error) {
	clusters, err := k3dclient.ClusterList(ctx, runtimes.SelectedRuntime)
	if err != nil {
		return false, diag.Wrap(err, diag.CodeK3dCreateFailed, "cannot list k3d clusters", "is the container runtime running?")
	}
	for _, c := range clusters {
		if c.Name == name {
			return true, nil
		}
	}
	return false, nil
}

// Delete tears down the named k3d cluster.
func (k *K3d) Delete(ctx context.Context, name string) error {
	if err := k3dclient.ClusterDelete(ctx, runtimes.SelectedRuntime, &k3dtypes.Cluster{Name: name}, k3dtypes.ClusterDeleteOpts{}); err != nil {
		return diag.Wrap(err, diag.CodeK3dDeleteFailed, fmt.Sprintf("failed to delete k3d cluster %q", name), "retry, or remove the container manually")
	}
	return nil
}

// Kubeconfig returns the kubeconfig for the named k3d cluster.
func (k *K3d) Kubeconfig(ctx context.Context, name string) ([]byte, error) {
	kcAPI, err := k3dclient.KubeconfigGet(ctx, runtimes.SelectedRuntime, &k3dtypes.Cluster{Name: name})
	if err != nil {
		return nil, diag.Wrap(err, diag.CodeK3dKubeconfigGet, "cannot get kubeconfig from k3d", "retry; if it persists, `cube-idp down` and `up` again")
	}
	return clientcmd.Write(*kcAPI)
}

// Diagnose reports non-fatal findings about the k3d/container runtime
// environment (`cube-idp doctor` surfaces these).
func (k *K3d) Diagnose(ctx context.Context, name string) []diag.Finding {
	if _, err := k3dclient.ClusterList(ctx, runtimes.SelectedRuntime); err != nil {
		return []diag.Finding{{Code: diag.CodeK3dCreateFailed, Severity: diag.SeverityError,
			Message:     "container runtime unreachable: " + err.Error(),
			Remediation: "start Docker/Podman and retry"}}
	}
	return nil
}
