package diag

import (
	"errors"
	"strings"
	"testing"
)

func TestErrorFormatsCodeAndCause(t *testing.T) {
	cause := errors.New("connection refused")
	err := Wrap(cause, "CUBE-1101", "cluster unreachable", "check that the kubeconfig context points at a running cluster")
	if got := err.Error(); !strings.Contains(got, "CUBE-1101") || !strings.Contains(got, "connection refused") {
		t.Fatalf("unexpected: %q", got)
	}
	if !errors.Is(err, cause) {
		t.Fatal("Unwrap chain broken")
	}
}

func TestRenderIncludesRemediation(t *testing.T) {
	err := New("CUBE-0002", "cube.yaml is invalid", "run `cube-idp config schema` to see the expected shape")
	out := Render(err)
	for _, want := range []string{"CUBE-0002", "cube.yaml is invalid", "fix:", "config schema"} {
		if !strings.Contains(out, want) {
			t.Fatalf("Render missing %q in:\n%s", want, out)
		}
	}
}

func TestRenderPlainError(t *testing.T) {
	if got := Render(errors.New("boom")); got != "Error: boom" {
		t.Fatalf("unexpected: %q", got)
	}
}
