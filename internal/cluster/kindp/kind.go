package kindp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"k8s.io/client-go/tools/clientcmd"
	kindcluster "sigs.k8s.io/kind/pkg/cluster"
	"sigs.k8s.io/kind/pkg/cluster/nodes"
	"sigs.k8s.io/kind/pkg/cluster/nodeutils"
	kindexec "sigs.k8s.io/kind/pkg/exec"

	"github.com/rafpe/cube-idp/internal/bundle"
	"github.com/rafpe/cube-idp/internal/config"
	"github.com/rafpe/cube-idp/internal/diag"
	"github.com/rafpe/cube-idp/internal/kube"
	"github.com/rafpe/cube-idp/internal/trust"
)

// loadRetryBackoff is the pause before the single retry of a per-node image
// import (F10: `ctr images import` was observed to fail transiently ~1-in-3
// runs in CI with no reproducible cause other than containerd/host timing).
const loadRetryBackoff = 2 * time.Second

// Kind implements cluster.Provider for local kind (kubernetes-in-docker)
// clusters (spec §4.1). It is a thin wrapper around sigs.k8s.io/kind: all
// config assembly happens in RenderConfig (merge.go), which stays pure and
// cluster-free.
type Kind struct {
	gw       config.GatewaySpec
	provider *kindcluster.Provider
}

// New returns a Kind provider bound to the given gateway spec. It
// autodetects the node backend (docker/podman/nerdctl, spec §4.1); if
// detection fails, the provider falls back to docker so that later calls
// (Exists/Diagnose) surface a clear CUBE-1203 rather than panicking here.
func New(gw config.GatewaySpec) *Kind {
	np, _ := kindcluster.DetectNodeProvider()
	var opts []kindcluster.ProviderOption
	if np != nil {
		opts = append(opts, np)
	}
	return &Kind{gw: gw, provider: kindcluster.NewProvider(opts...)}
}

// Ensure creates the named kind cluster if it doesn't already exist, then
// returns a connection to it.
func (k *Kind) Ensure(ctx context.Context, name string, spec config.ClusterSpec) (*kube.Conn, error) {
	exists, err := k.Exists(ctx, name)
	if err != nil {
		return nil, err
	}
	if !exists {
		certsd, err := k.certsD()
		if err != nil {
			return nil, err
		}
		cfg, err := RenderConfig(name, spec, k.gw, certsd)
		if err != nil {
			return nil, err
		}
		err = k.provider.Create(name,
			kindcluster.CreateWithRawConfig(cfg),
			kindcluster.CreateWithWaitForReady(120*time.Second),
		)
		if err != nil {
			return nil, diag.Wrap(err, diag.CodeKindCreateFailed, "kind cluster creation failed",
				"check that the container runtime is running and has free resources; `cube-idp doctor` will preflight this")
		}
	}
	kc, err := k.provider.KubeConfig(name, false)
	if err != nil {
		return nil, diag.Wrap(err, diag.CodeKindKubeconfigGet, "cannot get kubeconfig from kind", "retry; if it persists, `cube-idp down` and `up` again")
	}
	restCfg, err := clientcmd.RESTConfigFromKubeConfig([]byte(kc))
	if err != nil {
		return nil, diag.Wrap(err, diag.CodeKindKubeconfigGet, "kind kubeconfig is invalid", "delete the cluster with `cube-idp down` and retry")
	}
	return &kube.Conn{Kubeconfig: []byte(kc), Context: "kind-" + name, REST: restCfg}, nil
}

// certsD prepares the containerd certs.d host directory that maps
// registry.<gw.Host> image refs on kind nodes to the zot NodePort (D6
// canonical hostname, Task 10). It depends on the trust package's CA (D12:
// EnsureCA runs before cluster creation in up.Run) so certs.d exists before
// RenderConfig ever mounts it.
func (k *Kind) certsD() (CertsD, error) {
	dir, err := trust.Dir()
	if err != nil {
		return CertsD{}, err
	}
	ca, err := trust.EnsureCA(dir)
	if err != nil {
		return CertsD{}, err
	}
	host := "registry." + k.gw.Host
	hostDir := filepath.Join(dir, "certsd", host)
	if err := trust.WriteCertsD(hostDir, host, "http://localhost:30500", ca.CertPath); err != nil {
		return CertsD{}, err
	}
	return CertsD{Host: host, HostDir: hostDir}, nil
}

// LoadImages implements cluster.ImageLoader: it streams every bundled
// per-image OCI-layout tar into each of the named cluster's nodes' containerd
// runtime, so `up --bundle` pods start without any registry pull. Images load
// in ascending ref order (bundle.SortedImageLoads) for deterministic progress
// output; each tar is streamed once per node (kind runs one node here, but
// the loop is node-count-agnostic). Any failure — no such cluster, an
// unreadable tar, or a runtime import that rejects the layout — wraps as
// CUBE-7002 naming the image, never a silent skip.
//
// A single `ctr images import` call is retried once (loadWithRetry, F10):
// this failure mode was observed to be transient roughly 1-in-3 times in CI,
// and the retry alone resolves it without operator intervention. The
// remediation names that transience explicitly rather than pointing at the
// bundle, which is not at fault.
func (k *Kind) LoadImages(ctx context.Context, name string, imageTars map[string]string) error {
	nodeList, err := k.provider.ListNodes(name)
	if err != nil {
		return diag.Wrap(err, diag.CodeVendorPullFail,
			fmt.Sprintf("cannot list nodes of kind cluster %q to load bundled images", name),
			"verify the cluster exists (`cube-idp status`) and the container runtime is running, then retry")
	}
	for _, img := range bundle.SortedImageLoads(imageTars) {
		if err := loadArchiveIntoNodes(nodeList, img.Tar, openAndLoadArchive, loadRetryBackoff); err != nil {
			return diag.Wrap(err, diag.CodeVendorPullFail,
				fmt.Sprintf("cannot load image %s into cluster nodes", img.Ref),
				"transient containerd import failure — re-run `cube-idp up --bundle` (idempotent); if it persists, verify the bundle with `cube-idp vendor` on a connected machine and retry")
		}
	}
	return nil
}

// imageArchiveLoad is the retry seam for importing one image archive into
// one kind node. Production wires it to openAndLoadArchive; tests inject a
// fake that fails a fixed number of times, exercising loadWithRetry without a
// real cluster.
type imageArchiveLoad func(n nodes.Node, path string) error

// openAndLoadArchive opens the tar at path and hands the reader to kind's
// nodeutils.LoadImageArchive (the `kind load image-archive` primitive: a
// containerd import of an OCI-layout tar).
func openAndLoadArchive(n nodes.Node, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return nodeutils.LoadImageArchive(n, f)
}

// loadArchiveIntoNodes runs load once per node, retrying each per the rules
// in loadWithRetry. A fresh reader is opened per attempt (openAndLoadArchive
// re-opens the file), so a failed first attempt never leaves a partially-read
// stream behind for the retry.
func loadArchiveIntoNodes(nodeList []nodes.Node, path string, load imageArchiveLoad, backoff time.Duration) error {
	for _, n := range nodeList {
		if err := loadWithRetry(n, path, load, backoff); err != nil {
			return err
		}
	}
	return nil
}

// loadWithRetry runs load, and on failure retries exactly once after
// backoff. If both attempts fail, the returned error surfaces the underlying
// command's stderr: kind's exec.RunError captures stdout/stderr in its
// Output field, but its Error() string only reports the exit status —
// without unwrapping to RunError here, the operator sees "exit status 1"
// with no clue why the import failed.
func loadWithRetry(n nodes.Node, path string, load imageArchiveLoad, backoff time.Duration) error {
	err := load(n, path)
	if err == nil {
		return nil
	}
	time.Sleep(backoff)
	if err2 := load(n, path); err2 == nil {
		return nil
	} else {
		err = err2
	}
	return withStderr(err)
}

// withStderr appends the captured command output from a kind exec.RunError
// (if any) to the error message, since RunError.Error() otherwise omits it.
func withStderr(err error) error {
	var runErr *kindexec.RunError
	if errors.As(err, &runErr) && len(runErr.Output) > 0 {
		return fmt.Errorf("%w (output: %s)", err, strings.TrimSpace(string(runErr.Output)))
	}
	return err
}

// Exists reports whether a kind cluster with the given name exists.
func (k *Kind) Exists(ctx context.Context, name string) (bool, error) {
	names, err := k.provider.List()
	if err != nil {
		return false, diag.Wrap(err, diag.CodeKindCreateFailed, "cannot list kind clusters", "is the container runtime running?")
	}
	return slices.Contains(names, name), nil
}

// Delete tears down the named kind cluster.
func (k *Kind) Delete(ctx context.Context, name string) error {
	if err := k.provider.Delete(name, ""); err != nil {
		return diag.Wrap(err, diag.CodeKindDeleteFailed, fmt.Sprintf("failed to delete kind cluster %q", name), "retry, or remove the container manually")
	}
	return nil
}

// Kubeconfig returns the kubeconfig for the named kind cluster.
func (k *Kind) Kubeconfig(ctx context.Context, name string) ([]byte, error) {
	kc, err := k.provider.KubeConfig(name, false)
	if err != nil {
		return nil, diag.Wrap(err, diag.CodeKindKubeconfigGet, "cannot get kubeconfig from kind", "retry; if it persists, `cube-idp down` and `up` again")
	}
	return []byte(kc), nil
}

// Diagnose reports non-fatal findings about the kind/container runtime
// environment (Phase 2's `doctor` command surfaces these).
func (k *Kind) Diagnose(ctx context.Context, name string) []diag.Finding {
	if _, err := k.provider.List(); err != nil {
		return []diag.Finding{{Code: diag.CodeKindCreateFailed, Severity: diag.SeverityError,
			Message:     "container runtime unreachable: " + err.Error(),
			Remediation: "start Docker/Podman and retry"}}
	}
	return nil
}
