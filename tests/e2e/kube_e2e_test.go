// Package e2e composes domains against real infrastructure exactly like
// the CLI edge does — cluster seam plus kube client, no domain importing
// another. Opt-in via `make test-e2e` (CUBE_E2E=1, worktree-local
// KUBECONFIG); never part of the green gate.
package e2e

import (
	"os"
	"os/exec"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/cube-idp/cube-idp/internal/cluster"
	"github.com/cube-idp/cube-idp/internal/cluster/kind"
	"github.com/cube-idp/cube-idp/internal/kube"
)

// TestKubeClientRoundTrip provisions a kind cluster through the seam,
// injects the seam's kubeconfig bytes into the kube domain, and exercises
// every exported client against the live API server: Ping, discovery,
// RESTMapper resolution, and a dynamic list. Teardown via the seam.
func TestKubeClientRoundTrip(t *testing.T) {
	if os.Getenv("CUBE_E2E") != "1" {
		t.Skip("e2e is opt-in: run via `make test-e2e` (sets CUBE_E2E=1)")
	}
	if !runtimeAvailable() {
		t.Skip("no container runtime reachable (docker/podman) — skipping e2e")
	}
	const name = "cube-kube-e2e"
	ctx := t.Context()
	p, err := kind.New()
	if err != nil {
		t.Fatalf("kind.New: %v", err)
	}
	t.Cleanup(func() { _ = p.Delete(ctx, name) }) // safety net; the real Delete asserts below
	if err := p.Ensure(ctx, cluster.Spec{Name: name}); err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	raw, err := p.Kubeconfig(ctx, name)
	if err != nil {
		t.Fatalf("Kubeconfig: %v", err)
	}
	client, err := kube.New(raw, "") // injected bytes, provider-native current-context
	if err != nil {
		t.Fatalf("kube.New: %v", err)
	}

	if err := client.Ping(ctx); err != nil {
		t.Fatalf("Ping: %v", err)
	}
	version, err := client.Discovery().ServerVersion()
	if err != nil {
		t.Fatalf("Discovery().ServerVersion(): %v", err)
	}
	t.Logf("api server version: %s", version)

	mapping, err := client.RESTMapper().RESTMapping(schema.GroupKind{Kind: "ConfigMap"}, "v1")
	if err != nil {
		t.Fatalf("RESTMapper().RESTMapping(ConfigMap, v1): %v", err)
	}
	if mapping.Resource.Resource != "configmaps" {
		t.Errorf("RESTMapping(ConfigMap, v1).Resource = %q, want %q", mapping.Resource.Resource, "configmaps")
	}

	list, err := client.Dynamic().Resource(mapping.Resource).Namespace("kube-system").List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("Dynamic() list of kube-system configmaps: %v", err)
	}
	if len(list.Items) == 0 {
		t.Error("Dynamic() list of kube-system configmaps = 0 items, want at least one")
	}

	if err := p.Delete(ctx, name); err != nil {
		t.Fatalf("Delete: %v", err)
	}
}

// runtimeAvailable mirrors the kind driver's opt-in probe: e2e runs only
// where a container runtime answers.
func runtimeAvailable() bool {
	for _, rt := range []string{"docker", "podman"} {
		if exec.Command(rt, "info").Run() == nil {
			return true
		}
	}
	return false
}
