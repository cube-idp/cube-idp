package cmd

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRenderEngineRendersPack: the command prints the engine pack render —
// same objects `up` would SSA (engine-as-pack §3.3.10). The fixture is a
// manifests-only cube-engine-flux pack (nil values) pointed at by
// spec.engine.ref, since the published 0.1.0 default does not resolve until
// the engine packs are published to the public registry.
func TestRenderEngineRendersPack(t *testing.T) {
	dir := t.TempDir()
	pd := filepath.Join(dir, "cube-engine-flux")
	if err := os.MkdirAll(filepath.Join(pd, "manifests"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pd, "pack.cue"),
		[]byte("name: \"cube-engine-flux\"\nversion: \"0.1.0\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A distinctive namespace name the retired embedded flux blob does NOT
	// contain — so this test fails against the old InstallManifests() source
	// and only passes when render-engine renders the pack at spec.engine.ref.
	if err := os.WriteFile(filepath.Join(pd, "manifests", "ns.yaml"),
		[]byte("apiVersion: v1\nkind: Namespace\nmetadata:\n  name: enginepack-fixture-ns\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cy := filepath.Join(dir, "cube.yaml")
	if err := os.WriteFile(cy, []byte(fmt.Sprintf(`apiVersion: cube-idp.dev/v1alpha1
kind: Cube
metadata: {name: t}
spec:
  engine: {type: flux, ref: %s}
  gateway: {host: cube-idp.localtest.me, port: 8443, pack: traefik}
`, pd)), 0o644); err != nil {
		t.Fatal(err)
	}

	root := NewRootCmd()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"config", "render-engine", "-f", cy})
	if err := root.Execute(); err != nil {
		t.Fatalf("render-engine: %v (stderr: %s)", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "kind: Namespace") || !strings.Contains(stdout.String(), "enginepack-fixture-ns") {
		t.Fatalf("render-engine must print the pack render (at spec.engine.ref), got:\n%s", stdout.String())
	}
}

// TestRenderClusterNotesCertsDInjection covers (g): render-cluster's output
// is a pure/file-free rendering (cmd/config.go's comment: "no certs.d
// staging here"), so it genuinely omits the containerd certs.d bind mount
// `up` injects into the real cluster config at create-time
// (internal/cluster/kindp/merge.go, D6 canonical hostname). That gap must
// be surfaced to the user, not just documented in a code comment — but
// stdout must stay pure YAML (render-cluster's output is meant to be piped
// straight into `kind create cluster --config -`), so the note belongs on
// stderr.
func TestRenderClusterNotesCertsDInjection(t *testing.T) {
	t.Chdir(t.TempDir())

	root := NewRootCmd()
	root.SetOut(&bytes.Buffer{})
	root.SetArgs([]string{"init", "--name", "dev"})
	if err := root.Execute(); err != nil {
		t.Fatalf("init: %v", err)
	}

	root = NewRootCmd()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"config", "render-cluster"})
	if err := root.Execute(); err != nil {
		t.Fatalf("render-cluster: %v", err)
	}

	if !strings.Contains(stdout.String(), "kind.x-k8s.io/v1alpha4") {
		t.Fatalf("stdout must be the rendered kind config YAML, got:\n%s", stdout.String())
	}
	if strings.Contains(stdout.String(), "certs.d") {
		t.Fatalf("stdout must stay pure YAML — the certs.d note belongs on stderr, got stdout:\n%s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "certs.d") {
		t.Fatalf("expected a stderr note that certs.d is injected at `up` time, got:\n%s", stderr.String())
	}
}

func TestRenderClusterPrintsOverrideWarnings(t *testing.T) {
	// cube.yaml whose forProvider sets a conflicting node image: render
	// must succeed, stdout must be the final YAML with the core image,
	// stderr must carry a CUBE-1206 line (stdout stays pipeable YAML).
	t.Chdir(t.TempDir())
	doc := `apiVersion: cube-idp.dev/v1alpha1
kind: Cube
metadata:
  name: dev
spec:
  cluster:
    provider: kind
    kubernetesVersion: v1.33.1
    forProvider:
      nodes:
      - role: control-plane
        image: kindest/node:v1.99.0
  engine:
    type: flux
  gateway:
    pack: traefik
    host: cube-idp.localtest.me
    port: 8443
`
	if err := os.WriteFile("cube.yaml", []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	root := NewRootCmd()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"config", "render-cluster"})
	if err := root.Execute(); err != nil {
		t.Fatalf("render-cluster: %v", err)
	}
	if !strings.Contains(stdout.String(), "kindest/node:v1.33.1") ||
		strings.Contains(stdout.String(), "v1.99.0") {
		t.Fatalf("core image must win in stdout:\n%s", stdout.String())
	}
	if strings.Contains(stdout.String(), "CUBE-1206") {
		t.Fatalf("stdout must stay pure YAML:\n%s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "CUBE-1206") {
		t.Fatalf("stderr must carry the override warning:\n%s", stderr.String())
	}
}
