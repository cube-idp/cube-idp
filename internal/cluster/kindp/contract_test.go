package kindp_test

import (
	"context"
	"os"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/cube-idp/cube-idp/internal/cluster/contracttest"
	"github.com/cube-idp/cube-idp/internal/cluster/kindp"
	"github.com/cube-idp/cube-idp/internal/config"
	"github.com/cube-idp/cube-idp/internal/diag"
)

func TestKindProviderContract(t *testing.T) {
	gw := config.GatewaySpec{Pack: "traefik", Host: "cube-idp.localtest.me", Port: 18443} // non-default port: avoid colliding with a dev cluster
	contracttest.Run(t, kindp.New(gw), config.ClusterSpec{Provider: "kind", KubernetesVersion: "v1.33.1"})
}

func TestRenderContract(t *testing.T) {
	base := config.ClusterSpec{Provider: "kind", KubernetesVersion: "v1.33.1"}
	conflict := base
	conflict.ForProvider = map[string]any{"nodes": []any{
		map[string]any{"role": "control-plane", "image": "kindest/node:v1.99.0"}}}
	contracttest.RenderContract(t, base, conflict,
		func(s config.ClusterSpec) ([]byte, []diag.Finding, error) {
			return kindp.RenderConfig(context.Background(), "contract", s, config.GatewaySpec{Pack: "traefik", Host: "h", Port: 8443}, kindp.CertsD{})
		})
}

// TestForProviderE2E is the live e2e smoke: a forProvider node
// label observed via the Kubernetes API proves the channel end-to-end
// through kubeadm; the accompanying featureGate proves the cluster still
// boots with one set. Gated — needs a container runtime.
func TestForProviderE2E(t *testing.T) {
	if os.Getenv("CUBE_IDP_PROVIDER_E2E") != "1" {
		t.Skip("set CUBE_IDP_PROVIDER_E2E=1 to run (needs a container runtime)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	spec := config.ClusterSpec{
		Provider: "kind", KubernetesVersion: "v1.33.1",
		ForProvider: map[string]any{
			"featureGates": map[string]any{"InPlacePodVerticalScaling": true},
			"nodes": []any{map[string]any{
				"role":   "control-plane",
				"labels": map[string]any{"cube-idp.dev/forprovider-e2e": "yes"}}},
		},
	}
	p := kindp.New(config.GatewaySpec{}) // zero gateway: spoke-style, no port mapping
	name := "forprovider-e2e"
	conn, err := p.Ensure(ctx, name, spec)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	t.Cleanup(func() { _ = p.Delete(context.Background(), name) })
	cs, err := kubernetes.NewForConfig(conn.REST)
	if err != nil {
		t.Fatal(err)
	}
	nodes, err := cs.CoreV1().Nodes().List(ctx, metav1.ListOptions{
		LabelSelector: "cube-idp.dev/forprovider-e2e=yes"})
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes.Items) != 1 {
		t.Fatalf("forProvider node label not observed: %d nodes matched", len(nodes.Items))
	}
}
