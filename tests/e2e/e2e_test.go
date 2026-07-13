// Package e2e proves the full cube-idp loop (spec §5, "E2E (CI)") against a
// real kind cluster using local docker: init -> up -> up (idempotency) ->
// status -> get secrets -> down. Gated by CUBE_IDP_E2E=1 since it needs
// docker and takes minutes (image pulls dominate).
package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// cubeName is both the cube's metadata.name and (per internal/cluster/kindp)
// its kind cluster name. Kept distinct from unrelated clusters that may be
// running on the same docker host (e.g. airbyte-e2e, airbyte-poc) — this
// test only ever touches a cluster literally named "e2e".
const cubeName = "e2e"

// Full loop on a real kind cluster: init -> up -> up (idempotency) ->
// status -> get secrets -> down.
// Requires docker; run locally with:
//
//	CUBE_IDP_E2E=1 go test ./tests/e2e/ -v -timeout 25m
func TestUpStatusDown(t *testing.T) {
	if os.Getenv("CUBE_IDP_E2E") != "1" {
		t.Skip("set CUBE_IDP_E2E=1 to run")
	}

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

	run(t, dir, bin, "init", "--name", cubeName, "--local", repoRoot)

	upStart := time.Now()
	run(t, dir, bin, "up") // must exit 0 (spec: diagnose loudly and exit)
	upWall := time.Since(upStart)
	t.Logf("cube-idp up wall time: %s (goal: <60s excluding image pulls, tracked not asserted)", upWall)

	run(t, dir, bin, "up") // idempotency: re-run is the upgrade command

	out := run(t, dir, bin, "status")
	for _, comp := range []string{"traefik", "gitea", "argocd"} {
		if !strings.Contains(out, comp) {
			t.Fatalf("status missing %s:\n%s", comp, out)
		}
	}

	secrets := run(t, dir, bin, "get", "secrets", "-p", "gitea")
	if !strings.Contains(secrets, "gitea_admin") {
		t.Fatalf("gitea admin secret not surfaced (D9):\n%s", secrets)
	}

	run(t, dir, bin, "down")
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
