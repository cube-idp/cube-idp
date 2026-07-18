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

// TestSpokeRemoveDeleteClusterYes replaces S1's stub contract: with --yes,
// --delete-cluster must reach the real provider deletion (through the
// spokeClusterDelete seam) for the GT7-named cluster — the S1 "ships in a
// later task" CUBE-8001 error is gone.
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
