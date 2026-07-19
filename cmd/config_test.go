package cmd

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

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
