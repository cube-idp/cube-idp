// Phase 3 e2e scenarios (spec §5, "E2E (CI)" widened to the {kind, k3d} x
// {flux, argocd} grid). These prove the Phase 3 surfaces end to end against a
// real cluster on local docker, and they are the ARBITERS for the phase's
// deliberately-deferred unknowns:
//
//   - TestK3dUpDown          — the provider matrix's second leg (k3d up/down),
//     plus k3d's zero-value ImageImportOpts is exercised transitively by the
//     bundle leg when CUBE_IDP_E2E_PROVIDER=k3d.
//   - TestVendorBundleOffline — containerd's acceptance of the bundle's
//     per-image OCI-layout tars via LoadImages (kind LoadImageArchive / k3d
//     ImageImportIntoClusterMulti). A test failure HERE is information: it
//     means the per-image OCI-layout tar shape is rejected and the fallback
//     recorded in internal/bundle is needed — do not paper over it.
//   - TestSyncOneShot        — sync one-shot delivery (D7).
//   - TestRepoCreateDeploy   — repo create --deploy end to end (git push over
//     the gateway -> engine syncs -> ConfigMap appears).
//   - TestEnvoyGatewaySmoke  — envoy-gateway as spec.gateway.pack with the
//     StrategicMerge NodePort-30443 pinning honored at runtime (Owner
//     Decisions #7 / Task 4).
//
// All are gated by CUBE_IDP_E2E=1. Helpers build/run/gatewayPort/
// patchGatewayPort/assertGatewayTLS/mustUserConfigDir live in e2e_test.go
// (same package). Provider is selected by CUBE_IDP_E2E_PROVIDER (default
// kind); TestK3dUpDown always forces k3d regardless, since it IS the k3d leg.
// Every cluster this file creates is named with an "e2e-" prefix and torn
// down in t.Cleanup — it never touches any other cluster on the host.
package e2e

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/yaml"

	"github.com/cube-idp/cube-idp/internal/cluster"
	"github.com/cube-idp/cube-idp/internal/config"
)

// gitea admin Secret facts (checkpoint 0.10/0.8, mirrored from cmd/repo.go's
// unexported constants): admin credential lives in Secret
// gitea-admin-cube-idp/namespace gitea, keys username/password.
const (
	e2eGiteaNamespace       = "gitea"
	e2eGiteaAdminSecretName = "gitea-admin-cube-idp"
)

// providerName is the cluster provider the matrix leg selects
// (CUBE_IDP_E2E_PROVIDER), defaulting to kind — the harness writes it into
// cube.yaml before `up`. TestK3dUpDown overrides this to k3d unconditionally.
func providerName() string {
	if p := os.Getenv("CUBE_IDP_E2E_PROVIDER"); p != "" {
		return p
	}
	return "kind"
}

// engineName is the gitops engine the matrix leg selects
// (CUBE_IDP_E2E_ENGINE), defaulting to flux — same default as e2e_test.go.
func engineName() string {
	if e := os.Getenv("CUBE_IDP_E2E_ENGINE"); e != "" {
		return e
	}
	return "flux"
}

func requireE2E(t *testing.T) {
	t.Helper()
	if os.Getenv("CUBE_IDP_E2E") != "1" {
		t.Skip("set CUBE_IDP_E2E=1 to run")
	}
}

func requireDocker(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not on PATH — Phase 3 e2e needs a container runtime")
	}
}

// initCube runs `init --name <name> --local <packs-checkout> --engine
// <engine>` in dir, then patches the just-written cube.yaml to the requested
// provider and gateway port (P4: --local points at a cube-idp/packs checkout
// — packsCheckout, tests/e2e/PACKS.md). `init` has no --provider flag (it
// always writes kind), so a non-kind leg is applied here via the same
// config.Cube round-trip patchGatewayPort uses — schema-valid because
// config.Load re-validates it on the next `up`.
func initCube(t *testing.T, dir, bin, name, provider string, port int) {
	t.Helper()
	run(t, dir, bin, "init", "--name", name, "--local", packsCheckout(t), "--engine", engineName())
	if provider != "kind" {
		patchCube(t, dir, func(c *config.Cube) { c.Spec.Cluster.Provider = provider })
	}
	patchGatewayPort(t, dir, port)
}

// patchCube loads dir/cube.yaml into a config.Cube, applies mutate, and writes
// it back. Same schema-safe round-trip as patchGatewayPort (e2e_test.go): the
// marshaled document stays loadable by config.Load because it goes through the
// exact struct `init` itself marshals.
func patchCube(t *testing.T, dir string, mutate func(*config.Cube)) {
	t.Helper()
	path := filepath.Join(dir, "cube.yaml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading cube.yaml: %v", err)
	}
	var cube config.Cube
	if err := yaml.Unmarshal(raw, &cube); err != nil {
		t.Fatalf("parsing cube.yaml: %v", err)
	}
	mutate(&cube)
	out, err := yaml.Marshal(&cube)
	if err != nil {
		t.Fatalf("marshaling patched cube.yaml: %v", err)
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatalf("writing patched cube.yaml: %v", err)
	}
}

// cleanupCube is the t.Cleanup teardown for a Phase 3 scenario: `down` is the
// primary path (it reads cube.yaml, cascades, and deletes the cluster via the
// Go provider), and guardDeleteCluster is a provider-CLI backstop for the case
// where `up` never left a usable cube.yaml/cluster pairing or `down` failed.
// Never touches any cluster but the named one.
func cleanupCube(t *testing.T, bin, dir, provider, name string) {
	t.Helper()
	down := exec.Command(bin, "down", "--yes")
	down.Dir = dir
	out, _ := down.CombinedOutput()
	t.Logf("cleanup: cube-idp down\n%s", out)
	guardDeleteCluster(t, provider, name)
}

// guardDeleteCluster best-effort deletes a cluster literally named name via
// the provider's CLI, if that CLI is on PATH. Log-only on failure — this is a
// backstop, never the primary teardown, and it never touches another cluster.
func guardDeleteCluster(t *testing.T, provider, name string) {
	t.Helper()
	var cmd *exec.Cmd
	switch provider {
	case "kind":
		if _, err := exec.LookPath("kind"); err != nil {
			return
		}
		cmd = exec.Command("kind", "delete", "cluster", "--name", name)
	case "k3d":
		if _, err := exec.LookPath("k3d"); err != nil {
			return
		}
		cmd = exec.Command("k3d", "cluster", "delete", name)
	default:
		return
	}
	out, err := cmd.CombinedOutput()
	t.Logf("guard: delete %s cluster %q\n%s", provider, name, out)
	if err != nil {
		t.Logf("guard: delete %s cluster %q failed (non-fatal): %v", provider, name, err)
	}
}

// runOut runs the binary in dir and returns combined output plus the error,
// WITHOUT fataling — for polling loops and for commands whose exit code is the
// signal under test. (run() in e2e_test.go fatals on any non-zero exit.)
func runOut(t *testing.T, bin, dir string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	t.Logf("$ cube-idp %s\n%s", strings.Join(args, " "), out)
	return string(out), err
}

// pollStatusReady polls `status` until it exits 0 (all packs Ready — the plain
// path exits non-zero while any pack is unready) AND names packName Ready, or
// fails after timeout. This is the kubectl-style poll the brief's sync/repo
// scenarios call for, expressed through the binary's own status surface.
func pollStatusReady(t *testing.T, bin, dir, packName string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last string
	for time.Now().Before(deadline) {
		out, err := runOut(t, bin, dir, "status")
		last = out
		if err == nil && strings.Contains(out, packName) && strings.Contains(out, "Ready") {
			return
		}
		time.Sleep(5 * time.Second)
	}
	t.Fatalf("pack %q never reached Ready within %s; last status:\n%s", packName, timeout, last)
}

// clusterClientset builds a real client-go clientset for the cube described by
// dir/cube.yaml, exactly the way the read-only commands do (config.Load ->
// cluster.New -> Provider.Ensure connects to the already-`up` cluster ->
// REST). Ensure on an existing cluster only connects; it never creates one.
func clusterClientset(t *testing.T, dir string) *kubernetes.Clientset {
	t.Helper()
	cube, err := config.Load(filepath.Join(dir, "cube.yaml"))
	if err != nil {
		t.Fatalf("loading cube.yaml: %v", err)
	}
	prov, err := cluster.New(cube.Spec.Cluster, cube.Spec.Gateway)
	if err != nil {
		t.Fatalf("building provider: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	conn, err := prov.Ensure(ctx, cube.Metadata.Name, cube.Spec.Cluster)
	if err != nil {
		t.Fatalf("connecting to cluster: %v", err)
	}
	cs, err := kubernetes.NewForConfig(conn.REST)
	if err != nil {
		t.Fatalf("building clientset: %v", err)
	}
	return cs
}

// waitConfigMap polls until a ConfigMap ns/name exists in-cluster or timeout.
func waitConfigMap(t *testing.T, cs *kubernetes.Clientset, ns, name string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		_, err := cs.CoreV1().ConfigMaps(ns).Get(ctx, name, metav1.GetOptions{})
		cancel()
		if err == nil {
			return
		}
		lastErr = err
		time.Sleep(5 * time.Second)
	}
	t.Fatalf("ConfigMap %s/%s never appeared within %s: %v", ns, name, timeout, lastErr)
}

// giteaAdminCreds reads the gitea pack's admin username/password from the
// gitea-admin-cube-idp Secret via client-go — the same Secret cmd/repo.go
// reads. Used to authenticate the test's `git push` over the gateway.
func giteaAdminCreds(t *testing.T, cs *kubernetes.Clientset) (user, pass string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	sec, err := cs.CoreV1().Secrets(e2eGiteaNamespace).Get(ctx, e2eGiteaAdminSecretName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("reading gitea admin secret: %v", err)
	}
	return string(sec.Data["username"]), string(sec.Data["password"])
}

// TestK3dUpDown is the provider matrix's second leg: init a cube, rewrite its
// provider to k3d, `up`, assert every pack is Ready (the plain `status` path
// exits non-zero unless all are — so a passing `status` IS the all-Ready
// assertion), then `down`. Mirrors the kind loop in TestUpStatusDown. Always
// k3d regardless of CUBE_IDP_E2E_PROVIDER, since it is the dedicated k3d leg.
func TestK3dUpDown(t *testing.T) {
	requireE2E(t)
	requireDocker(t)

	const provider = "k3d"
	name := "e2e-k3d"
	port := gatewayPort(t)
	dir := t.TempDir()
	bin := build(t)

	guardDeleteCluster(t, provider, name) // clear any lingering cluster first
	t.Cleanup(func() { cleanupCube(t, bin, dir, provider, name) })

	initCube(t, dir, bin, name, provider, port)

	run(t, dir, bin, "up")

	out := run(t, dir, bin, "status")
	for _, comp := range []string{"traefik", "gitea"} {
		if !strings.Contains(out, comp) {
			t.Fatalf("status missing %s on the k3d leg:\n%s", comp, out)
		}
	}
	if !strings.Contains(out, "Ready") {
		t.Fatalf("status reports no Ready packs on the k3d leg:\n%s", out)
	}

	// The k3d provider maps host gateway port -> NodePort 30443; the gateway
	// serves the cube-idp CA-issued cert (same probe as the kind/traefik leg).
	assertGatewayTLS(t, "gitea.cube-idp.localtest.me:"+strconv.Itoa(port))

	run(t, dir, bin, "down", "--yes")
}

// TestVendorBundleOffline is the air-gap honesty check AND the arbiter for the
// bundle's per-image OCI-layout tar acceptance by containerd (via kind
// LoadImageArchive / k3d ImageImportIntoClusterMulti). Flow: `up` (online) ->
// `vendor -o b.tgz` (reads cube.lock) -> `down` -> `up --bundle b.tgz`.
//
// Offline assertions on the bundle-up output, keyed on the per-pack
// "fetching <source>" step line internal/up emits with the RESOLVED fetch
// source (stepFetchSource, added by the Task 13 review — an online run
// demonstrably prints the network/checkout ref there, pinned by
// TestStepFetchSourcePlainOutput in internal/up): the output MUST contain the
// "image(s) loaded into cluster nodes" step (the node-load path ran — the
// shipped Task 7 wording is "▸ [bundle] N image(s) loaded into cluster
// nodes", which the plan draft paraphrased); EVERY
// "fetching" line's source must point into the bundle's cube-idp-bundle-*
// staging dir (positive: each pack was read from the bundle, not the network
// or the checkout — this would fail on any online run, whose sources are
// checkout paths or oci:// refs); and NO "fetching" line may name an oci://
// source (negative). A ref absent from the bundle is CUBE-7004, never a
// silent fetch. A true network-namespace cutoff is not feasible on shared
// runners; the ref-resolution guarantee plus these falsifiable output
// assertions are what CI can prove, and that limitation is stated in this
// comment on purpose. If `up --bundle` fails at LoadImages, that is the
// recorded arbiter result: containerd rejected the per-image OCI-layout tar
// shape and the internal/bundle fallback is required — report it, do not
// mask it.
func TestVendorBundleOffline(t *testing.T) {
	requireE2E(t)
	requireDocker(t)

	provider := providerName()
	if provider == "existing" {
		t.Skip("bundle install needs an image-loading provider (kind/k3d)")
	}
	name := "e2e-bundle"
	port := gatewayPort(t)
	dir := t.TempDir()
	bin := build(t)

	guardDeleteCluster(t, provider, name)
	t.Cleanup(func() { cleanupCube(t, bin, dir, provider, name) })

	initCube(t, dir, bin, name, provider, port)

	// Online up writes cube.lock, which vendor consumes.
	run(t, dir, bin, "up")
	bundlePath := filepath.Join(dir, "b.tgz")
	run(t, dir, bin, "vendor", "-o", bundlePath)
	if fi, err := os.Stat(bundlePath); err != nil || fi.Size() == 0 {
		t.Fatalf("vendor produced no bundle at %s: %v", bundlePath, err)
	}

	// Tear the cluster down and reinstall from the bundle alone.
	run(t, dir, bin, "down", "--yes")
	guardDeleteCluster(t, provider, name)

	out := run(t, dir, bin, "up", "--bundle", bundlePath)
	if !strings.Contains(out, "image(s) loaded into cluster nodes") {
		t.Fatalf("bundle up did not node-load images (missing offline load step):\n%s", out)
	}
	assertFetchSourcesFromBundle(t, out)
	// A converged bundle-installed cube is Ready like any other.
	run(t, dir, bin, "status")

	run(t, dir, bin, "down", "--yes")
}

// assertFetchSourcesFromBundle parses every per-pack resolved-fetch-source
// line ("▸ [pack] fetching <source>", emitted by internal/up's
// stepFetchSource) out of a `up --bundle` run's output and asserts, per line:
// the source points into the bundle's cube-idp-bundle-* staging dir
// (positive) and is not an oci:// ref (negative). At least one such line must
// exist — zero would mean the pack loop never ran or the line was renamed,
// either of which must fail loudly rather than pass vacuously.
func assertFetchSourcesFromBundle(t *testing.T, out string) {
	t.Helper()
	const marker = "fetching "
	count := 0
	for _, line := range strings.Split(out, "\n") {
		idx := strings.Index(line, marker)
		if idx < 0 || !strings.Contains(line, "[pack]") {
			continue
		}
		count++
		source := strings.TrimSpace(line[idx+len(marker):])
		if strings.HasPrefix(source, "oci://") {
			t.Fatalf("bundle up fetched a pack from the network (%s) — bundle install must be offline:\n%s", source, out)
		}
		if !strings.Contains(source, "cube-idp-bundle-") {
			t.Fatalf("bundle up fetched a pack from outside the bundle staging dir (%s):\n%s", source, out)
		}
	}
	if count == 0 {
		t.Fatalf("no per-pack \"fetching <source>\" lines found — the offline assertion would be vacuous:\n%s", out)
	}
}

// TestSyncOneShot proves D7's one-shot delivery: `up`, write a bare dir with a
// single ConfigMap, `sync <dir>`, poll `status` until the synthesized pack
// reports Ready, then assert the ConfigMap exists in-cluster (client-go). The
// synthesized pack's name is the sync dir's base name (loadOrSynthesize). Its
// inventory is covered by `down` (Task 10 / Owner Decisions #14 merge).
func TestSyncOneShot(t *testing.T) {
	requireE2E(t)
	requireDocker(t)

	provider := providerName()
	name := "e2e-sync"
	port := gatewayPort(t)
	dir := t.TempDir()
	bin := build(t)

	guardDeleteCluster(t, provider, name)
	t.Cleanup(func() { cleanupCube(t, bin, dir, provider, name) })

	initCube(t, dir, bin, name, provider, port)
	run(t, dir, bin, "up")

	// Bare manifest dir: no pack.cue, one ConfigMap YAML — synthesized into a
	// pack named after the directory ("synced").
	syncDir := filepath.Join(dir, "synced")
	if err := os.MkdirAll(syncDir, 0o755); err != nil {
		t.Fatal(err)
	}
	const cmName = "e2e-sync-cm"
	cmYAML := fmt.Sprintf("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: %s\n  namespace: default\ndata:\n  ok: \"true\"\n", cmName)
	if err := os.WriteFile(filepath.Join(syncDir, "cm.yaml"), []byte(cmYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	run(t, dir, bin, "sync", syncDir)
	pollStatusReady(t, bin, dir, "synced", 3*time.Minute)

	cs := clusterClientset(t, dir)
	waitConfigMap(t, cs, "default", cmName, 2*time.Minute)

	run(t, dir, bin, "down", "--yes")
}

// TestRepoCreateDeploy is the "empty repo to deployed" acceptance test, end to
// end: `up`, `repo create app --deploy` (creates a Gitea repo and registers it
// as an engine delivery source), then push a ConfigMap manifest to the new
// repo over the HTTPS gateway with the `git` CLI + the gitea admin credentials,
// and poll until the pushed ConfigMap appears in-cluster (the engine cloned
// the repo from inside the cluster and applied it).
func TestRepoCreateDeploy(t *testing.T) {
	requireE2E(t)
	requireDocker(t)
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH — repo create --deploy needs the git CLI to push")
	}

	provider := providerName()
	name := "e2e-repo"
	port := gatewayPort(t)
	dir := t.TempDir()
	bin := build(t)

	guardDeleteCluster(t, provider, name)
	t.Cleanup(func() { cleanupCube(t, bin, dir, provider, name) })

	initCube(t, dir, bin, name, provider, port)
	run(t, dir, bin, "up")

	const repoName = "app"
	run(t, dir, bin, "repo", "create", repoName, "--deploy")

	cs := clusterClientset(t, dir)
	user, pass := giteaAdminCreds(t, cs)

	// Clone-push a ConfigMap to the new repo over the gateway. The gateway
	// serves a cube-idp-CA cert git wouldn't otherwise trust; this is a local
	// test push, so verification is disabled rather than wiring the CA into
	// git's trust store.
	host := "gitea.cube-idp.localtest.me:" + strconv.Itoa(port)
	pushURL := fmt.Sprintf("https://%s:%s@%s/%s/%s.git", user, pass, host, user, repoName)
	work := filepath.Join(dir, "push")
	gitEnv := append(os.Environ(), "GIT_SSL_NO_VERIFY=1", "GIT_TERMINAL_PROMPT=0")

	runGit(t, "", gitEnv, "clone", pushURL, work)
	const cmName = "e2e-repo-cm"
	cmYAML := fmt.Sprintf("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: %s\n  namespace: default\ndata:\n  ok: \"true\"\n", cmName)
	if err := os.WriteFile(filepath.Join(work, "cm.yaml"), []byte(cmYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, work, gitEnv, "config", "user.email", "e2e@cube-idp.dev")
	runGit(t, work, gitEnv, "config", "user.name", "cube-idp e2e")
	runGit(t, work, gitEnv, "add", "cm.yaml")
	runGit(t, work, gitEnv, "commit", "-m", "e2e: deploy configmap")
	runGit(t, work, gitEnv, "push", "origin", "HEAD")

	waitConfigMap(t, cs, "default", cmName, 3*time.Minute)

	run(t, dir, bin, "down", "--yes")
}

// runGit runs the git CLI in workdir (empty = default) with env, fataling on
// error. Kept separate from run()/runOut() because git is a foreign binary,
// not the cube-idp binary.
func runGit(t *testing.T, workdir string, env []string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	if workdir != "" {
		cmd.Dir = workdir
	}
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	t.Logf("$ git %s\n%s", strings.Join(args, " "), out)
	if err != nil {
		t.Fatalf("git %v failed: %v", args, err)
	}
}

// TestEnvoyGatewaySmoke proves envoy-gateway as spec.gateway.pack (Owner
// Decisions #7): init, rewrite the gateway pack to envoy-gateway (pack + local
// Ref), `up`, assert every pack is Ready and the gateway answers TLS through
// the envoy data plane — the same cert probe the kind/traefik leg uses,
// proving the turnkey Gateway + NodePort-30443 StrategicMerge pinning (Task 4)
// is honored at runtime — then `down`.
func TestEnvoyGatewaySmoke(t *testing.T) {
	requireE2E(t)
	requireDocker(t)

	provider := providerName()
	name := "e2e-envoy"
	port := gatewayPort(t)
	dir := t.TempDir()
	bin := build(t)
	packsRoot := packsCheckout(t)

	guardDeleteCluster(t, provider, name)
	t.Cleanup(func() { cleanupCube(t, bin, dir, provider, name) })

	initCube(t, dir, bin, name, provider, port)
	// Swap the gateway pack from traefik to envoy-gateway. init --local set
	// Ref to the packs checkout's packs/traefik; repoint both Pack and Ref
	// so `up` fetches the envoy-gateway pack from the same checkout.
	patchCube(t, dir, func(c *config.Cube) {
		c.Spec.Gateway.Pack = "envoy-gateway"
		c.Spec.Gateway.Ref = filepath.Join(packsRoot, "packs", "envoy-gateway")
	})

	run(t, dir, bin, "up")

	out := run(t, dir, bin, "status")
	if !strings.Contains(out, "Ready") {
		t.Fatalf("status reports no Ready packs on the envoy leg:\n%s", out)
	}
	// The envoy data plane must serve the gateway on the pinned NodePort.
	assertGatewayTLS(t, "gitea.cube-idp.localtest.me:"+strconv.Itoa(port))

	// In-cluster *.<host> must be served by the DATA PLANE via the CoreDNS
	// rewrite (the F9/KNOWN-GAP flow): run a one-shot curl pod against
	// https://gitea.<host>:8443 and require success. Pre-R7b this resolved
	// to the envoy CONTROLLER Service and could never answer. Port 8443, not
	// 443: the Gateway's websecure listener (packs/{traefik,envoy-gateway}
	// manifests) and its Service both listen on 8443 — the host-side
	// NodePort mapping (18443 -> 30443 -> 8443, the port var used above) is
	// a host-only concern that doesn't exist for an in-cluster client
	// talking to the Service directly.
	assertInClusterHTTP(t, provider, name, dir, "https://gitea.cube-idp.localtest.me:8443")

	run(t, dir, bin, "down", "--yes")
}

// assertInClusterHTTP creates a curlimages/curl pod in the default namespace
// running `curl -fskS -o /dev/null <url>` (-k: the pod does not trust the
// cube CA; DNS + data-plane reachability is what's under test), polls its
// phase to Succeeded within 3 minutes (curlimages/curl is not pre-pulled, so
// the budget includes the first-ever image pull), and dumps a status summary
// plus logs on failure. The pod is deleted in t.Cleanup. provider/clusterName
// select which cube.yaml's cluster this dials (clusterClientset's Load ->
// cluster.New -> Ensure pattern); they are only used for the t.Fatalf message
// context here since the clientset itself is built from dir/cube.yaml exactly
// like clusterClientset.
func assertInClusterHTTP(t *testing.T, provider, clusterName, dir, url string) {
	t.Helper()
	cs := clusterClientset(t, dir)

	const ns = "default"
	podName := "e2e-inclusterhttp-probe"
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: podName, Namespace: ns},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyNever,
			Containers: []corev1.Container{
				{
					Name:    "curl",
					Image:   "curlimages/curl:8.10.1",
					Command: []string{"curl", "-fskS", "-o", "/dev/null", url},
				},
			},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// Best-effort delete of a stale pod from a previous failed run before
	// creating: Create fails AlreadyExists otherwise.
	_ = cs.CoreV1().Pods(ns).Delete(ctx, podName, metav1.DeleteOptions{})

	createCtx, createCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer createCancel()
	if _, err := cs.CoreV1().Pods(ns).Create(createCtx, pod, metav1.CreateOptions{}); err != nil {
		t.Fatalf("creating in-cluster HTTP probe pod (provider=%s cluster=%s): %v", provider, clusterName, err)
	}
	t.Cleanup(func() {
		delCtx, delCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer delCancel()
		_ = cs.CoreV1().Pods(ns).Delete(delCtx, podName, metav1.DeleteOptions{})
	})

	// 3 minutes: curlimages/curl is not pre-pulled/mirrored on the kind node
	// (unlike the pack images `up` loads ahead of time), so the first-ever
	// pull from Docker Hub is part of the budget, not just the curl itself.
	var last *corev1.Pod
	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		getCtx, getCancel := context.WithTimeout(context.Background(), 20*time.Second)
		got, err := cs.CoreV1().Pods(ns).Get(getCtx, podName, metav1.GetOptions{})
		getCancel()
		if err != nil {
			t.Fatalf("polling in-cluster HTTP probe pod: %v", err)
		}
		last = got
		switch got.Status.Phase {
		case corev1.PodSucceeded:
			return
		case corev1.PodFailed:
			t.Fatalf("in-cluster HTTP probe of %s failed (pod %s/%s Failed):\n%s\n%s", url, ns, podName, podStatusSummary(last), podLogs(t, cs, ns, podName))
		}
		time.Sleep(3 * time.Second)
	}
	t.Fatalf("in-cluster HTTP probe of %s never reached Succeeded within 3m:\n%s\n%s", url, podStatusSummary(last), podLogs(t, cs, ns, podName))
}

// podStatusSummary renders p's phase and each container's waiting/running/
// terminated reason — the detail that distinguishes "still pulling the
// image" from "crash-looping" from "actually can't resolve/reach the URL",
// none of which podLogs alone (empty pre-start) can tell apart.
func podStatusSummary(p *corev1.Pod) string {
	if p == nil {
		return "(no pod status observed)"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "phase=%s", p.Status.Phase)
	for _, cs := range p.Status.ContainerStatuses {
		switch {
		case cs.State.Waiting != nil:
			fmt.Fprintf(&b, " container=%s waiting=%s(%s)", cs.Name, cs.State.Waiting.Reason, cs.State.Waiting.Message)
		case cs.State.Running != nil:
			fmt.Fprintf(&b, " container=%s running", cs.Name)
		case cs.State.Terminated != nil:
			fmt.Fprintf(&b, " container=%s terminated=%s(%s) exitCode=%d", cs.Name, cs.State.Terminated.Reason, cs.State.Terminated.Message, cs.State.Terminated.ExitCode)
		}
	}
	return b.String()
}

// podLogs best-effort fetches ns/name's container logs for a probe-pod
// failure message — logged, not fataled, since this only runs while already
// building a t.Fatalf message.
func podLogs(t *testing.T, cs *kubernetes.Clientset, ns, name string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	raw, err := cs.CoreV1().Pods(ns).GetLogs(name, &corev1.PodLogOptions{}).DoRaw(ctx)
	if err != nil {
		return fmt.Sprintf("(failed to fetch logs: %v)", err)
	}
	return string(raw)
}
