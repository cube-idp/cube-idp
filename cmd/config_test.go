package cmd

import (
	"bytes"
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
