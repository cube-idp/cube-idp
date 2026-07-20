// Package e2e proves the full cube-idp loop end to end in CI against a
// real kind cluster using local docker: init -> doctor -> up -> up
// (idempotency) -> diff -> upgrade --plan -> status -> kubectl get packs ->
// cnoe import -> get secrets -> down. Gated by CUBE_IDP_E2E=1 since it needs
// docker and takes minutes (image pulls dominate). Runs across the
// {flux, argocd} engine matrix (CUBE_IDP_E2E_ENGINE, default "flux") per
// the kind x {flux, argocd} x {up, diff, upgrade, down} CI matrix.
package e2e

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"sigs.k8s.io/yaml"

	"github.com/cube-idp/cube-idp/internal/config"
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

	packsRoot := packsCheckout(t)

	t.Cleanup(func() {
		// Best-effort: `down` is the primary teardown path (it cascades and
		// deletes the kind cluster); the kind guard below is a backstop in
		// case `up` never got far enough to leave a usable cube.yaml/cluster
		// pairing, or `down` itself failed.
		downCmd := exec.Command(bin, "down", "--yes")
		downCmd.Dir = dir
		out, _ := downCmd.CombinedOutput()
		t.Logf("cleanup: cube-idp down\n%s", out)
		deleteLingeringCluster(t)
	})

	// The `init` invocation (incl. its --local resolution) is kept exactly
	// as `cube-idp init` itself defines it, with --engine appended. The
	// packs live in the cube-idp/packs monorepo, so --local points at a
	// packs checkout (tests/e2e/PACKS.md), not this repo.
	run(t, dir, bin, "init", "--name", cubeName, "--local", packsRoot, "--engine", eng)
	patchGatewayPort(t, dir, port)

	run(t, dir, bin, "doctor") // preflights must pass on a clean runner

	upStart := time.Now()
	run(t, dir, bin, "up") // must exit 0 (spec: diagnose loudly and exit)
	recordUpWallTime(t, eng, time.Since(upStart))

	// Registry-reachability check: a kind node's containerd can pull
	// registry.<host>/... through certs.d + the zot NodePort (never through
	// localtest.me, which resolves to the node itself from inside a node —
	// see internal/trust/certsd.go). Engine-independent (kind provider +
	// containerd config), so checked once on the flux leg only.
	if eng == "flux" {
		assertNodeCanPullFromRegistry(t, "packs/traefik:0.2.0")
	}

	run(t, dir, bin, "up") // idempotency: re-run is the upgrade command

	// cube.lock written and well-formed
	lockRaw, err := os.ReadFile(dir + "/cube.lock")
	if err != nil || !strings.Contains(string(lockRaw), "renderedHash") {
		t.Fatalf("cube.lock missing or malformed: %v\n%s", err, lockRaw)
	}

	// A converged cube has no diff and no pending upgrades (exit 0)
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

	// HTTPS gateway — TLS handshake serves the cube-idp CA-issued cert
	assertGatewayTLS(t, "cube-idp.localtest.me:"+strconv.Itoa(port))

	// D11: pack records are discoverable via plain kubectl
	packs := runKubectl(t, "get", "packs")
	for _, want := range []string{"gitea", "VERSION", "URL", "AUTH-SECRET", "READY"} {
		if !strings.Contains(packs, want) {
			t.Fatalf("kubectl get packs missing %q (D11 printer columns):\n%s", want, packs)
		}
	}
	// The rendered URL must carry the gateway's actual port —
	// the pre-fix ${GATEWAY_HOST} substitution injected only the host, so
	// the printed link dialed the default HTTPS port (443) instead of
	// wherever the gateway actually listens, and was dead.
	if wantSuffix := ":" + strconv.Itoa(port); !strings.Contains(packs, wantSuffix) {
		t.Fatalf("kubectl get packs URL column missing gateway port %q (Task 15.1):\n%s", wantSuffix, packs)
	}

	// engine-as-pack (§3.3.7): the engine installs from the cube-engine-<type>
	// pack and `up` writes its Pack record like any other pack — but with
	// DELIVERY "engine" (up.go: PackObject(enginePk, …, "engine", nil)), the
	// bit that distinguishes the CLI-applied engine install from oci/repo pack
	// delivery. Assert the row exists for THIS engine and carries that value.
	enginePack := "cube-engine-" + eng
	engineDelivery := strings.TrimSpace(runKubectl(t, "get", "pack", enginePack, "-o", "jsonpath={.spec.delivery}"))
	if engineDelivery != "engine" {
		t.Fatalf("engine Pack record %q: DELIVERY = %q, want %q (§3.3.7 engine record row)\nkubectl get packs:\n%s", enginePack, engineDelivery, "engine", packs)
	}

	// cnoe-compat import round-trips
	writeCnoeFixture(t, dir)
	run(t, dir, bin, "cnoe", "import", dir+"/cnoe-apps")

	// D9 + D11: the admin credential surfaces via the Pack -> authSecretRef
	// pivot, and gitea_admin arrives through expose.impliedFields
	secrets := run(t, dir, bin, "get", "secrets", "-p", "gitea")
	if !strings.Contains(secrets, "gitea_admin") {
		t.Fatalf("gitea admin secret not surfaced (D9/D11):\n%s", secrets)
	}
	run(t, dir, bin, "down", "--yes")
}

// TestPackDependsOn proves the p6 dep-chain leg end-to-end on a real kind
// cluster (flux engine): a cube.yaml-level `packs[].dependsOn` (DEP1-3)
// actually orders delivery in the cluster and is echoed back on the pack's
// D11 record (DEP4). init --local writes the default profile's packs in
// declared order [gitea, argocd] (cmd/init.go); this test patches the
// argocd entry to declare `dependsOn: ["gitea"]` — the cube.yaml surface,
// since published packs don't declare their own dependsOn yet (DEP5). The
// flux-specific kubectl assertion (Kustomization spec.dependsOn) is why
// this test does not run the {flux, argocd} engine matrix like
// TestUpStatusDown — it pins the flux engine, the default. Requires
// docker; run locally with:
//
//	CUBE_IDP_E2E=1 CUBE_IDP_E2E_GATEWAY_PORT=18443 go test ./tests/e2e/ -run TestPackDependsOn -v -timeout 25m
func TestPackDependsOn(t *testing.T) {
	if os.Getenv("CUBE_IDP_E2E") != "1" {
		t.Skip("set CUBE_IDP_E2E=1 to run")
	}
	port := gatewayPort(t)

	// Guard against a lingering cluster from a previous aborted e2e run —
	// this test uses the same cluster name as TestUpStatusDown, so the two
	// must never run concurrently against the same docker host (standard
	// `go test` sequential-by-default behavior makes that true here).
	deleteLingeringCluster(t)

	bin := build(t)
	dir := t.TempDir()
	packsRoot := packsCheckout(t)

	t.Cleanup(func() {
		downCmd := exec.Command(bin, "down", "--yes")
		downCmd.Dir = dir
		out, _ := downCmd.CombinedOutput()
		t.Logf("cleanup: cube-idp down\n%s", out)
		deleteLingeringCluster(t)
	})

	run(t, dir, bin, "init", "--name", cubeName, "--local", packsRoot, "--engine", "flux")
	patchGatewayPort(t, dir, port)
	patchCube(t, dir, func(c *config.Cube) {
		// Default profile order is [gitea, argocd] (cmd/init.go) — find
		// argocd by ref substring rather than assuming index 1, so this
		// test does not silently stop exercising the real path if the
		// default profile's order ever changes.
		found := false
		for i := range c.Spec.Packs {
			if strings.Contains(c.Spec.Packs[i].Ref, "argocd") {
				c.Spec.Packs[i].DependsOn = []string{"gitea"}
				found = true
			}
		}
		if !found {
			t.Fatalf("default profile has no argocd pack ref to patch: %+v", c.Spec.Packs)
		}
	})

	run(t, dir, bin, "up")

	// (a) flux Kustomization ordering: argocd's Kustomization carries a
	// dependsOn on gitea's, by the cube-idp-<pack> naming convention
	// (internal/engine/flux/deliver.go deliveryName).
	got := strings.TrimSpace(runKubectl(t, "get", "kustomization", "cube-idp-argocd",
		"-n", "flux-system", "-o", "jsonpath={.spec.dependsOn[0].name}"))
	if got != "cube-idp-gitea" {
		t.Fatalf("argocd Kustomization spec.dependsOn[0].name = %q, want %q", got, "cube-idp-gitea")
	}

	// (b) D11 Pack record echoes the resolved dep back as the DEPENDS-ON
	// column's backing field (p6 DEP4).
	gotDep := strings.TrimSpace(runKubectl(t, "get", "packs", "argocd", "-o", "jsonpath={.spec.dependsOn}"))
	if gotDep != "gitea" {
		t.Fatalf("packs/argocd spec.dependsOn = %q, want %q", gotDep, "gitea")
	}

	// (c) status/health converge as usual — the dep chain didn't wedge
	// delivery or leave anything unhealthy.
	out := run(t, dir, bin, "status")
	for _, comp := range []string{"traefik", "gitea", "argocd"} {
		if !strings.Contains(out, comp) {
			t.Fatalf("status missing %s after the dep-chain leg:\n%s", comp, out)
		}
	}

	run(t, dir, bin, "down", "--yes")
}

// recordUpWallTime records the tracked CI metric for `up` wall time. The <60s goal
// is now scoped to a WARM run (node images already cached — see README's
// mounts:-based node-image cache recipe; this repo does no pre-pull
// engineering itself, so CI's own runs are typically cold and expected to
// exceed 60s). t.Logf keeps the number visible in -v output; when running
// under GitHub Actions (GITHUB_STEP_SUMMARY set), the same line is also
// appended to the job's step summary — visible in the Actions UI without
// digging through verbose logs, and diffable run over run. Best-effort: a
// summary-file write failure only degrades to log-only, never fails the
// test — this is telemetry, not a correctness assertion.
func recordUpWallTime(t *testing.T, engine string, wall time.Duration) {
	t.Helper()
	line := fmt.Sprintf("cube-idp up wall time (engine=%s): %s (goal: <60s warm, spec §3; tracked not asserted)", engine, wall)
	t.Logf("%s", line)
	summary := os.Getenv("GITHUB_STEP_SUMMARY")
	if summary == "" {
		return
	}
	f, err := os.OpenFile(summary, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Logf("could not open GITHUB_STEP_SUMMARY %s: %v (metric only logged above)", summary, err)
		return
	}
	defer f.Close()
	if _, err := fmt.Fprintf(f, "- %s\n", line); err != nil {
		t.Logf("could not write GITHUB_STEP_SUMMARY %s: %v (metric only logged above)", summary, err)
	}
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
// least-certain assumption in the canonical-hostname design — the fallback
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
//
// It POLLS (5s interval, gatewayTLSTimeout deadline, pollStatusReady's
// shape) rather than dialing once: `up` returns when the ENGINE reports the
// gateway pack Ready, but with envoy-gateway the pack's Ready component is
// the controller — the Envoy data-plane Deployment is created by that
// controller asynchronously afterwards (proxy image pull + xDS sync +
// listener programming), so a single immediate dial raced it and reset
// (found on the envoy-gateway e2e leg, 2026-07-15, after the pack's xDS
// Service-name collision was fixed). Kept as the one shared helper (same
// signature) instead of an envoy-only wrapper: the traefik path's gateway
// is already serving when `up` returns, so its first dial succeeds and
// polling costs nothing there, while any future engine/pack timing shift
// gets the same de-flake for free.
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
	deadline := time.Now().Add(gatewayTLSTimeout)
	var lastErr error
	for time.Now().Before(deadline) {
		conn, err := tls.Dial("tcp", addr, &tls.Config{RootCAs: pool, ServerName: "gitea.cube-idp.localtest.me"})
		if err == nil {
			conn.Close()
			return
		}
		lastErr = err
		time.Sleep(5 * time.Second)
	}
	t.Fatalf("TLS handshake with the gateway never succeeded within %s; last error: %v", gatewayTLSTimeout, lastErr)
}

// gatewayTLSTimeout bounds assertGatewayTLS's poll. Sized for the envoy
// data-plane's worst case on a fresh kind node (proxy image pull dominates);
// a hard deadline — every wait ends in a rendered diagnosis, never an
// infinite spinner (see docs/adr/0030-typed-cube-diagnostics.md).
const gatewayTLSTimeout = 3 * time.Minute

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

// packsCheckout resolves the local cube-idp/packs checkout the e2e suite
// feeds to `init --local` (the packs no longer live in this repo).
// CUBE_IDP_E2E_PACKS_DIR points at the checkout's packs/ directory; unset,
// it defaults to the sibling checkout's ../cube-idp-packs/packs (relative
// to this repo's root). Returns the checkout ROOT — the packs dir's parent,
// the shape `init --local` expects (it joins <root>/packs/<name>). The
// calling test SKIPS with a clone hint when the directory is missing — see
// tests/e2e/PACKS.md.
func packsCheckout(t *testing.T) string {
	t.Helper()
	dir := os.Getenv("CUBE_IDP_E2E_PACKS_DIR")
	if dir == "" {
		repoRoot, err := filepath.Abs("../..")
		if err != nil {
			t.Fatalf("resolving repo root: %v", err)
		}
		dir = filepath.Join(repoRoot, "..", "cube-idp-packs", "packs")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		t.Fatalf("resolving packs dir %q: %v", dir, err)
	}
	if st, err := os.Stat(abs); err != nil || !st.IsDir() {
		t.Skipf("no cube-idp/packs checkout at %s — clone it "+
			"(git clone https://github.com/cube-idp/packs ../cube-idp-packs) "+
			"or set CUBE_IDP_E2E_PACKS_DIR to a checkout's packs/ directory "+
			"(see tests/e2e/PACKS.md)", abs)
	}
	return filepath.Dir(abs)
}

// deleteLingeringCluster removes a kind cluster literally named cubeName if
// one exists. It never touches any other cluster (e.g. other kind clusters
// that may be running on this docker host for unrelated projects).
func deleteLingeringCluster(t *testing.T) {
	t.Helper()
	deleteLingeringClusterNamed(t, cubeName)
}

// deleteLingeringClusterNamed is deleteLingeringCluster for an explicit
// cluster name — the spoke e2e leg guards both the hub ("e2e") and its
// spoke cluster, named "<cube>-spoke-<name>" ("e2e-spoke-<name>" here).
func deleteLingeringClusterNamed(t *testing.T, name string) {
	t.Helper()
	if kindClusterExists(t, name) {
		del := exec.Command("kind", "delete", "cluster", "--name", name)
		delOut, delErr := del.CombinedOutput()
		t.Logf("guard: kind delete cluster --name %s\n%s", name, delOut)
		if delErr != nil {
			t.Logf("guard: kind delete cluster --name %s failed (non-fatal): %v", name, delErr)
		}
	}
}

// kindClusterExists reports whether a kind cluster with exactly this name
// exists; kind absent from PATH (or no clusters) reads as false.
func kindClusterExists(t *testing.T, name string) bool {
	t.Helper()
	out, err := exec.Command("kind", "get", "clusters").CombinedOutput()
	if err != nil {
		// kind not on PATH, or no clusters at all — nothing to see.
		return false
	}
	return slices.Contains(slices.Collect(strings.FieldsSeq(string(out))), name)
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

// TestRecordUpWallTimeWritesStepSummary and TestRecordUpWallTimeNoopWithoutStepSummary
// cover the tracked-metric helper directly — no docker/cluster
// needed, so these run unconditionally (not gated by CUBE_IDP_E2E).
func TestRecordUpWallTimeWritesStepSummary(t *testing.T) {
	summary := filepath.Join(t.TempDir(), "summary.md")
	t.Setenv("GITHUB_STEP_SUMMARY", summary)

	recordUpWallTime(t, "flux", 42*time.Second)

	raw, err := os.ReadFile(summary)
	if err != nil {
		t.Fatalf("expected GITHUB_STEP_SUMMARY to be written: %v", err)
	}
	got := string(raw)
	if !strings.Contains(got, "flux") || !strings.Contains(got, "42s") {
		t.Fatalf("expected the engine and wall time in the step summary, got:\n%s", got)
	}
}

func TestRecordUpWallTimeAppendsAcrossCalls(t *testing.T) {
	summary := filepath.Join(t.TempDir(), "summary.md")
	t.Setenv("GITHUB_STEP_SUMMARY", summary)

	recordUpWallTime(t, "flux", time.Second)
	recordUpWallTime(t, "argocd", 2*time.Second)

	raw, err := os.ReadFile(summary)
	if err != nil {
		t.Fatal(err)
	}
	got := string(raw)
	if !strings.Contains(got, "flux") || !strings.Contains(got, "argocd") {
		t.Fatalf("expected both engine matrix legs recorded (appended, not overwritten), got:\n%s", got)
	}
}

func TestRecordUpWallTimeNoopWithoutStepSummary(t *testing.T) {
	t.Setenv("GITHUB_STEP_SUMMARY", "") // unset: local runs never have this
	recordUpWallTime(t, "flux", time.Second)
	// Only requirement: must not panic or fail the test (t.Logf-only path).
}

// TestSpokeKindRegistration proves the S3 hub/spoke loop end-to-end on real
// kind clusters (registration only): declare one kind spoke, `up`
// creates + bootstraps it and registers it with the hub engine (hub secret
// cube-idp-spoke-<name> with a non-empty engine-native payload), then
// `down --yes` cascades the spoke cluster away. Gated like TestUpStatusDown
// (CUBE_IDP_E2E=1, docker required) and honoring CUBE_IDP_E2E_GATEWAY_PORT
// (a dev machine may already have 8443 bound). Run locally with:
//
//	CUBE_IDP_E2E=1 CUBE_IDP_E2E_GATEWAY_PORT=18443 go test ./tests/e2e/ -run TestSpokeKindRegistration -v -timeout 25m
func TestSpokeKindRegistration(t *testing.T) {
	if os.Getenv("CUBE_IDP_E2E") != "1" {
		t.Skip("set CUBE_IDP_E2E=1 to run")
	}
	eng := os.Getenv("CUBE_IDP_E2E_ENGINE")
	if eng == "" {
		eng = "flux"
	}
	port := gatewayPort(t)
	const spokeName = "staging"
	spokeCluster := cubeName + "-spoke-" + spokeName

	deleteLingeringCluster(t)
	deleteLingeringClusterNamed(t, spokeCluster)

	bin := build(t)
	dir := t.TempDir()
	packsRoot := packsCheckout(t)
	t.Cleanup(func() {
		downCmd := exec.Command(bin, "down", "--yes")
		downCmd.Dir = dir
		out, _ := downCmd.CombinedOutput()
		t.Logf("cleanup: cube-idp down\n%s", out)
		deleteLingeringCluster(t)
		deleteLingeringClusterNamed(t, spokeCluster)
	})

	run(t, dir, bin, "init", "--name", cubeName, "--local", packsRoot, "--engine", eng)
	patchGatewayPort(t, dir, port)
	run(t, dir, bin, "spoke", "add", spokeName, "--provider", "kind")

	run(t, dir, bin, "up")

	// Hub registration: the engine-native secret exists with a non-empty
	// payload (flux: kubeconfig under `value`; argocd: cluster `config`).
	ns, key := "flux-system", "value"
	if eng == "argocd" {
		ns, key = "argocd", "config"
	}
	payload := runKubectl(t, "get", "secret", "cube-idp-spoke-"+spokeName, "-n", ns,
		"-o", "jsonpath={.data."+key+"}")
	if strings.TrimSpace(payload) == "" {
		t.Fatalf("hub secret cube-idp-spoke-%s has an empty %q payload", spokeName, key)
	}

	// Spoke bootstrap: the RBAC namespace exists on the spoke cluster itself.
	runKubectl(t, "--context", "kind-"+spokeCluster, "get", "ns", "cube-idp-system")

	run(t, dir, bin, "down", "--yes")
	if kindClusterExists(t, spokeCluster) {
		t.Fatalf("spoke kind cluster %s survived down", spokeCluster)
	}
}

// TestPublishedPacksByDigest is the digest-pin leg: the e2e
// consumes the PUBLISHED packs repo pinned by digest, never by mutable tag.
// Gated on CUBE_IDP_E2E_ONLINE=1 (it pulls from ghcr.io — network + docker
// required) and on tests/e2e/packs.lock, the committed JSON map of
// name -> oci://…@sha256:… refs seeded by the owner after each publish
// (tests/e2e/PACKS.md). While the lock file is absent the test SKIPS.
func TestPublishedPacksByDigest(t *testing.T) {
	if os.Getenv("CUBE_IDP_E2E_ONLINE") != "1" {
		t.Skip("set CUBE_IDP_E2E_ONLINE=1 to run the online digest-pinned leg")
	}
	raw, err := os.ReadFile("packs.lock")
	if err != nil {
		t.Skipf("tests/e2e/packs.lock not present — seed it from the published digests "+
			"(name -> oci://…@sha256:… JSON; see tests/e2e/PACKS.md): %v", err)
	}
	lock := map[string]string{}
	if err := yaml.Unmarshal(raw, &lock); err != nil { // YAML superset: parses the JSON lock
		t.Fatalf("parsing packs.lock: %v", err)
	}
	for _, name := range []string{"traefik", "gitea"} {
		ref, ok := lock[name]
		if !ok {
			t.Fatalf("packs.lock missing %q (the online leg ups gateway+gitea)", name)
		}
		if !strings.Contains(ref, "@sha256:") {
			t.Fatalf("packs.lock[%q] = %q is not digest-pinned (decision 2)", name, ref)
		}
	}
	port := gatewayPort(t)

	deleteLingeringCluster(t)
	bin := build(t)
	dir := t.TempDir()
	t.Cleanup(func() {
		downCmd := exec.Command(bin, "down", "--yes")
		downCmd.Dir = dir
		out, _ := downCmd.CombinedOutput()
		t.Logf("cleanup: cube-idp down\n%s", out)
		deleteLingeringCluster(t)
	})

	run(t, dir, bin, "init", "--name", cubeName)
	patchCube(t, dir, func(c *config.Cube) {
		c.Spec.Gateway.Ref = lock["traefik"]
		c.Spec.Packs = []config.PackRef{{Ref: lock["gitea"]}}
	})
	patchGatewayPort(t, dir, port)

	run(t, dir, bin, "up")
	out := run(t, dir, bin, "status") // exits 1 iff any component is unhealthy
	for _, comp := range []string{"traefik", "gitea"} {
		if !strings.Contains(out, comp) {
			t.Fatalf("status missing %s on the digest-pinned leg:\n%s", comp, out)
		}
	}
	run(t, dir, bin, "down", "--yes")
}
