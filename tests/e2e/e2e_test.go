// Package e2e proves the full cube-idp loop (spec §5, "E2E (CI)") against a
// real kind cluster using local docker: init -> doctor -> up -> up
// (idempotency) -> diff -> upgrade --plan -> status -> kubectl get packs ->
// cnoe import -> get secrets -> down. Gated by CUBE_IDP_E2E=1 since it needs
// docker and takes minutes (image pulls dominate). Runs across the
// {flux, argocd} engine matrix (CUBE_IDP_E2E_ENGINE, default "flux") per
// spec §5's kind x {flux, argocd} x {up, diff, upgrade, down} matrix.
package e2e

import (
	"crypto/tls"
	"crypto/x509"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"sigs.k8s.io/yaml"

	"github.com/rafpe/cube-idp/internal/config"
)

// cubeName is both the cube's metadata.name and (per internal/cluster/kindp)
// its kind cluster name. Kept distinct from unrelated clusters that may be
// running on the same docker host (e.g. airbyte-e2e, airbyte-poc, testowy)
// — this test only ever touches a cluster literally named "e2e".
const cubeName = "e2e"

// gatewayPort is the host port the gateway is dialed on. CI always uses
// cube-idp's default (8443, matching spec D12's port-mapping). Locally, a
// host may already have something bound to 0.0.0.0:8443 (this repo's own
// dev machine has an unrelated kind cluster doing exactly that) —
// CUBE_IDP_E2E_GATEWAY_PORT lets a local run pick a free port instead
// without touching any other cluster. The generated cube.yaml's
// spec.gateway.port is patched to match before `up` runs, so kind's
// injected host<->node port mapping (internal/cluster/kindp/merge.go)
// follows the override.
func gatewayPort(t *testing.T) int {
	t.Helper()
	if v := os.Getenv("CUBE_IDP_E2E_GATEWAY_PORT"); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil {
			t.Fatalf("CUBE_IDP_E2E_GATEWAY_PORT=%q: %v", v, err)
		}
		return p
	}
	return 8443
}

// Full loop on a real kind cluster: init -> doctor -> up -> up (idempotency)
// -> diff -> upgrade --plan -> status -> kubectl get packs -> cnoe import ->
// get secrets -> down.
// Requires docker; run locally with:
//
//	CUBE_IDP_E2E=1 CUBE_IDP_E2E_ENGINE=flux go test ./tests/e2e/ -v -timeout 25m
func TestUpStatusDown(t *testing.T) {
	if os.Getenv("CUBE_IDP_E2E") != "1" {
		t.Skip("set CUBE_IDP_E2E=1 to run")
	}
	eng := os.Getenv("CUBE_IDP_E2E_ENGINE")
	if eng == "" {
		eng = "flux"
	}
	port := gatewayPort(t)

	// Guard against a lingering cluster from a previous aborted e2e run
	// before we start — never touch any other cluster on this host.
	deleteLingeringCluster(t)

	bin := build(t)
	dir := t.TempDir()

	repoRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("resolving repo root: %v", err)
	}

	t.Cleanup(func() {
		// Best-effort: `down` is the primary teardown path (it cascades and
		// deletes the kind cluster); the kind guard below is a backstop in
		// case `up` never got far enough to leave a usable cube.yaml/cluster
		// pairing, or `down` itself failed.
		downCmd := exec.Command(bin, "down")
		downCmd.Dir = dir
		out, _ := downCmd.CombinedOutput()
		t.Logf("cleanup: cube-idp down\n%s", out)
		deleteLingeringCluster(t)
	})

	// Phase 1 `init` invocation (incl. its --local resolution) kept exactly
	// as checkpoint 0.12 found it; --engine (Task 2) is appended.
	run(t, dir, bin, "init", "--name", cubeName, "--local", repoRoot, "--engine", eng)
	patchGatewayPort(t, dir, port)

	run(t, dir, bin, "doctor") // preflights must pass on a clean runner

	upStart := time.Now()
	run(t, dir, bin, "up") // must exit 0 (spec: diagnose loudly and exit)
	upWall := time.Since(upStart)
	t.Logf("cube-idp up wall time (engine=%s): %s (goal: <60s excluding image pulls, tracked not asserted)", eng, upWall)

	// Task 10 deferred verification: a kind node's containerd can pull
	// registry.<host>/... through certs.d + the zot NodePort (never through
	// localtest.me, which resolves to the node itself from inside a node —
	// see internal/trust/certsd.go). Engine-independent (kind provider +
	// containerd config), so checked once on the flux leg only.
	if eng == "flux" {
		assertNodeCanPullFromRegistry(t, "packs/traefik:0.1.0")
	}

	run(t, dir, bin, "up") // idempotency: re-run is the upgrade command

	// Phase 2: cube.lock written and well-formed
	lockRaw, err := os.ReadFile(dir + "/cube.lock")
	if err != nil || !strings.Contains(string(lockRaw), "renderedHash") {
		t.Fatalf("cube.lock missing or malformed: %v\n%s", err, lockRaw)
	}

	// Phase 2: a converged cube has no diff and no pending upgrades (exit 0)
	run(t, dir, bin, "diff")
	run(t, dir, bin, "upgrade", "--plan")

	out := run(t, dir, bin, "status")
	for _, comp := range []string{"traefik", "gitea"} {
		if !strings.Contains(out, comp) {
			t.Fatalf("status missing %s:\n%s", comp, out)
		}
	}
	if eng == "flux" { // argocd engine drops the redundant UI pack (CUBE-0005)
		if !strings.Contains(out, "argocd") {
			t.Fatalf("status missing argocd pack:\n%s", out)
		}
	}

	// Phase 2: HTTPS gateway — TLS handshake serves the cube-idp CA-issued cert
	assertGatewayTLS(t, "cube-idp.localtest.me:"+strconv.Itoa(port))

	// Phase 2 (D11): pack records are discoverable via plain kubectl
	packs := runKubectl(t, "get", "packs")
	for _, want := range []string{"gitea", "VERSION", "URL", "AUTH-SECRET", "READY"} {
		if !strings.Contains(packs, want) {
			t.Fatalf("kubectl get packs missing %q (D11 printer columns):\n%s", want, packs)
		}
	}
	// Task 15.1: the rendered URL must carry the gateway's actual port —
	// the pre-fix ${GATEWAY_HOST} substitution injected only the host, so
	// the printed link dialed the default HTTPS port (443) instead of
	// wherever the gateway actually listens, and was dead.
	if wantSuffix := ":" + strconv.Itoa(port); !strings.Contains(packs, wantSuffix) {
		t.Fatalf("kubectl get packs URL column missing gateway port %q (Task 15.1):\n%s", wantSuffix, packs)
	}

	// Phase 2: cnoe-compat import round-trips
	writeCnoeFixture(t, dir)
	run(t, dir, bin, "cnoe", "import", dir+"/cnoe-apps")

	// D9 + D11: the admin credential surfaces via the Pack -> authSecretRef
	// pivot, and gitea_admin arrives through expose.impliedFields
	secrets := run(t, dir, bin, "get", "secrets", "-p", "gitea")
	if !strings.Contains(secrets, "gitea_admin") {
		t.Fatalf("gitea admin secret not surfaced (D9/D11):\n%s", secrets)
	}
	run(t, dir, bin, "down")
}

// patchGatewayPort rewrites the just-written cube.yaml's spec.gateway.port,
// so a local run can dodge a host port already bound by an unrelated
// cluster (CUBE_IDP_E2E_GATEWAY_PORT) while CI keeps the real default
// (8443). Uses the same config.Cube shape `init` itself writes, so the
// round-trip stays schema-valid (schema.cue's gateway.port accepts any
// 1-65535 value, not just 8443).
func patchGatewayPort(t *testing.T, dir string, port int) {
	t.Helper()
	if port == 8443 {
		return // matches what `init` already wrote — nothing to do
	}
	path := filepath.Join(dir, "cube.yaml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading cube.yaml: %v", err)
	}
	var cube config.Cube
	if err := yaml.Unmarshal(raw, &cube); err != nil {
		t.Fatalf("parsing cube.yaml: %v", err)
	}
	cube.Spec.Gateway.Port = port
	out, err := yaml.Marshal(&cube)
	if err != nil {
		t.Fatalf("marshaling patched cube.yaml: %v", err)
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatalf("writing patched cube.yaml: %v", err)
	}
	t.Logf("patched cube.yaml: gateway.port=%d (CUBE_IDP_E2E_GATEWAY_PORT override)", port)
}

// runKubectl asserts against the cluster with plain kubectl (the D11 pitch
// is literally "kubectl get packs works"). GitHub runners ship kubectl;
// locally the test skips if it is absent. kind's Ensure wrote the context
// into the default kubeconfig.
func runKubectl(t *testing.T, args ...string) string {
	t.Helper()
	if _, err := exec.LookPath("kubectl"); err != nil {
		t.Skip("kubectl not on PATH")
	}
	out, err := exec.Command("kubectl", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("kubectl %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// assertNodeCanPullFromRegistry execs into the kind control-plane node
// (kind's container-naming convention: "<cluster>-control-plane") and pulls
// ref (e.g. "packs/traefik:0.1.0") through the containerd certs.d host
// mapping `up` wrote (internal/trust.WriteCertsD): registry.<gw.host>
// resolved via the zot NodePort on the node's own loopback
// (http://localhost:30500), never through localtest.me (which resolves to
// the node itself, not the gateway, from inside a node). This is the
// weakest guess flagged in the Phase 2 plan (Task 10) — the fallback design
// (rewrite hosts.toml with the node's InternalIP after create) only matters
// if this fails. Skips (not fails) if docker or the node's crictl are
// unavailable, so the suite degrades gracefully on a host without a usable
// container runtime rather than masquerading as a real check.
func assertNodeCanPullFromRegistry(t *testing.T, ref string) {
	t.Helper()
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not on PATH — cannot exec into the kind node to verify certs.d reachability (Task 10)")
	}
	node := cubeName + "-control-plane"
	full := "registry.cube-idp.localtest.me/" + ref
	out, err := exec.Command("docker", "exec", node, "crictl", "pull", full).CombinedOutput()
	if err != nil {
		t.Fatalf("kind node %s could not pull %s via certs.d + the zot NodePort (D6/Task 10 — see internal/trust/certsd.go): %v\n%s",
			node, full, err, out)
	}
	t.Logf("Task 10 verified: kind node %s pulled %s via certs.d + the zot NodePort\n%s", node, full, out)
}

// assertGatewayTLS dials the gateway and verifies the served cert chains to
// the cube-idp local CA and covers the wildcard host — the D6 story minus
// the OS trust store (never touched in CI).
func assertGatewayTLS(t *testing.T, addr string) {
	t.Helper()
	caPath := filepath.Join(mustUserConfigDir(t), "cube-idp", "ca.crt")
	caPEM, err := os.ReadFile(caPath)
	if err != nil {
		t.Fatalf("cube-idp CA missing after up: %v", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		t.Fatal("cannot parse cube-idp CA")
	}
	conn, err := tls.Dial("tcp", addr, &tls.Config{RootCAs: pool, ServerName: "gitea.cube-idp.localtest.me"})
	if err != nil {
		t.Fatalf("TLS handshake with the gateway failed: %v", err)
	}
	conn.Close()
}

func writeCnoeFixture(t *testing.T, dir string) {
	t.Helper()
	appDir := filepath.Join(dir, "cnoe-apps", "manifests")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(appDir, "cm.yaml"),
		[]byte("apiVersion: v1\nkind: ConfigMap\nmetadata: {name: cnoe-smoke}\ndata: {ok: \"true\"}\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "cnoe-apps", "app.yaml"), []byte(`apiVersion: argoproj.io/v1alpha1
kind: Application
metadata: {name: smoke}
spec:
  destination: {namespace: cnoe-smoke, server: https://kubernetes.default.svc}
  source: {repoURL: "cnoe://manifests", targetRevision: HEAD, path: "."}
`), 0o644)
}

func mustUserConfigDir(t *testing.T) string {
	t.Helper()
	d, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// deleteLingeringCluster removes a kind cluster literally named cubeName if
// one exists. It never touches any other cluster (e.g. other kind clusters
// that may be running on this docker host for unrelated projects).
func deleteLingeringCluster(t *testing.T) {
	t.Helper()
	out, err := exec.Command("kind", "get", "clusters").CombinedOutput()
	if err != nil {
		// kind not on PATH, or no clusters at all — nothing to clean up.
		return
	}
	for _, name := range strings.Fields(string(out)) {
		if name == cubeName {
			del := exec.Command("kind", "delete", "cluster", "--name", cubeName)
			delOut, delErr := del.CombinedOutput()
			t.Logf("guard: kind delete cluster --name %s\n%s", cubeName, delOut)
			if delErr != nil {
				t.Logf("guard: kind delete cluster --name %s failed (non-fatal): %v", cubeName, delErr)
			}
			return
		}
	}
}

func build(t *testing.T) string {
	t.Helper()
	bin := t.TempDir() + "/cube-idp"
	cmd := exec.Command("go", "build", "-o", bin, "../..")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	return bin
}

func run(t *testing.T, dir, bin string, args ...string) string {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	t.Logf("$ cube-idp %s\n%s", strings.Join(args, " "), out)
	if err != nil {
		t.Fatalf("cube-idp %v failed: %v", args, err)
	}
	return string(out)
}
