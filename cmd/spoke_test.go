package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cube-idp/cube-idp/internal/config"
)

// runCLI drives the root command the way every test in this package does
// (NewRootCmd + SetOut/SetErr/SetIn + Execute — pack_test.go's mechanics;
// no shared helper existed to reuse) and returns the combined output.
// Stdin is an empty buffer: non-TTY, so no prompt may ever engage.
func runCLI(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetIn(&bytes.Buffer{})
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

func mustRunCLI(t *testing.T, args ...string) string {
	t.Helper()
	out, err := runCLI(t, args...)
	if err != nil {
		t.Fatalf("cube-idp %v: %v\noutput: %s", args, err, out)
	}
	return out
}

func writeSpokeFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "cube.yaml")
	base := `apiVersion: cube-idp.dev/v1alpha1
kind: Cube
metadata: {name: dev}
spec:
  engine: {type: flux}
  gateway: {pack: traefik, host: cube-idp.localtest.me, port: 8443}
`
	if err := os.WriteFile(p, []byte(base), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestSpokeAddWritesConfig(t *testing.T) {
	p := writeSpokeFixture(t)
	out, err := runCLI(t, "spoke", "add", "staging", "--provider", "kind", "-f", p)
	if err != nil {
		t.Fatalf("spoke add: %v\n%s", err, out)
	}
	b, _ := os.ReadFile(p)
	if !strings.Contains(string(b), "spokes:") || !strings.Contains(string(b), "name: staging") {
		t.Fatalf("cube.yaml missing spoke:\n%s", b)
	}
	// Idempotent: adding the same name again fails cleanly, file unchanged.
	if _, err := runCLI(t, "spoke", "add", "staging", "--provider", "kind", "-f", p); err == nil {
		t.Fatal("duplicate spoke add must fail")
	}
}

func TestSpokeListAndRemove(t *testing.T) {
	p := writeSpokeFixture(t)
	mustRunCLI(t, "spoke", "add", "staging", "--provider", "kind", "-f", p)
	out := mustRunCLI(t, "spoke", "list", "-f", p)
	if !strings.Contains(out, "staging") || !strings.Contains(out, "kind") {
		t.Fatalf("spoke list missing row:\n%s", out)
	}
	mustRunCLI(t, "spoke", "remove", "staging", "-f", p)
	b, _ := os.ReadFile(p)
	if strings.Contains(string(b), "staging") {
		t.Fatalf("spoke not removed:\n%s", b)
	}
}

// TestSpokeListLiveColumns (S4): with a reachable hub, `spoke list` gains
// Registered/Reachable columns from the same collector status uses —
// paired glyph+word cells (semantic-color doctrine), no degradation note.
func TestSpokeListLiveColumns(t *testing.T) {
	stubStatusConnect(t, statusSnapshot{
		Spokes: []spokeStatus{{Name: "staging", Provider: "kind", Registered: true, Reachable: true}},
	})
	p := writeSpokeFixture(t)
	mustRunCLI(t, "spoke", "add", "staging", "--provider", "kind", "-f", p)
	out := mustRunCLI(t, "spoke", "list", "-f", p)
	for _, want := range []string{"staging", "kind", "✔ registered", "✔ reachable"} {
		if !strings.Contains(out, want) {
			t.Fatalf("live spoke list missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "hub unreachable") {
		t.Fatalf("live list must not print the degradation note:\n%s", out)
	}
}

// TestSpokeListDegradesWithoutHub (S4): when the hub cluster cannot be
// reached, `spoke list` still prints the declared config (the S1 table)
// plus a trailing note — graceful, exit 0, never an error.
func TestSpokeListDegradesWithoutHub(t *testing.T) {
	// Hermetic kubeconfig: the fixture's hub is `existing` with a context
	// that cannot exist, so the real statusConnect fails fast and offline.
	t.Setenv("KUBECONFIG", filepath.Join(t.TempDir(), "absent"))
	dir := t.TempDir()
	p := filepath.Join(dir, "cube.yaml")
	base := `apiVersion: cube-idp.dev/v1alpha1
kind: Cube
metadata: {name: dev}
spec:
  cluster: {provider: existing, context: no-such-context}
  engine: {type: flux}
  gateway: {pack: traefik, host: cube-idp.localtest.me, port: 8443}
  spokes:
    - name: staging
      cluster: {provider: kind}
`
	if err := os.WriteFile(p, []byte(base), 0o644); err != nil {
		t.Fatal(err)
	}
	out := mustRunCLI(t, "spoke", "list", "-f", p)
	for _, want := range []string{"staging", "kind", "hub unreachable — showing declared config only"} {
		if !strings.Contains(out, want) {
			t.Fatalf("degraded spoke list missing %q:\n%s", want, out)
		}
	}
	// Declared config only: no live state cells (the note's own
	// "unreachable" word aside, no glyphs and no registered column).
	if strings.Contains(out, "✔") || strings.Contains(out, "✗") || strings.Contains(out, "registered") {
		t.Fatalf("degraded list must show declared config only:\n%s", out)
	}
}

// TestSpokeRemoveDeleteClusterYes replaces S1's stub contract: with --yes,
// --delete-cluster must reach the real provider deletion (through the
// spokeClusterDelete seam) for the cluster named <cube>-spoke-<spoke> — the
// earlier "ships in a
// later release" CUBE-8001 error is gone.
func TestSpokeRemoveDeleteClusterYes(t *testing.T) {
	var deleted []string
	restore := spokeClusterDelete
	spokeClusterDelete = func(_ context.Context, _ config.SpokeSpec, name string) error {
		deleted = append(deleted, name)
		return nil
	}
	defer func() { spokeClusterDelete = restore }()

	p := writeSpokeFixture(t)
	mustRunCLI(t, "spoke", "add", "staging", "--provider", "kind", "-f", p)
	out := mustRunCLI(t, "spoke", "remove", "staging", "--delete-cluster", "--yes", "-f", p)
	if len(deleted) != 1 || deleted[0] != "dev-spoke-staging" {
		t.Fatalf("expected dev-spoke-staging deleted via the seam, got %v\noutput: %s", deleted, out)
	}
	if !strings.Contains(out, "dev-spoke-staging deleted") {
		t.Fatalf("deletion must be reported:\n%s", out)
	}
	b, _ := os.ReadFile(p)
	if strings.Contains(string(b), "staging") {
		t.Fatalf("spoke not removed from config:\n%s", b)
	}
}
