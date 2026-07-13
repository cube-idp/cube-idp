package cluster

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/rafpe/cube-idp/internal/config"
	"github.com/rafpe/cube-idp/internal/diag"
)

const kubeconfigTmpl = `
apiVersion: v1
kind: Config
clusters:
- name: dead
  cluster: {server: "https://127.0.0.1:1"}
contexts:
- name: dead-ctx
  context: {cluster: dead, user: u}
users:
- name: u
  user: {}
current-context: dead-ctx
`

func writeKubeconfig(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "kubeconfig")
	if err := os.WriteFile(p, []byte(kubeconfigTmpl), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestFactoryRejectsUnknownProvider(t *testing.T) {
	_, err := New(config.ClusterSpec{Provider: "minikube"}, config.GatewaySpec{})
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != "CUBE-1001" {
		t.Fatalf("want CUBE-1001, got %v", err)
	}
}

func TestExistingMissingContext(t *testing.T) {
	t.Setenv("KUBECONFIG", writeKubeconfig(t))
	p, err := New(config.ClusterSpec{Provider: "existing", Context: "nope"}, config.GatewaySpec{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.Ensure(context.Background(), "dev", config.ClusterSpec{Provider: "existing", Context: "nope"})
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != "CUBE-1102" {
		t.Fatalf("want CUBE-1102 (context not found), got %v", err)
	}
}

func TestExistingUnreachable(t *testing.T) {
	t.Setenv("KUBECONFIG", writeKubeconfig(t))
	p, _ := New(config.ClusterSpec{Provider: "existing", Context: "dead-ctx"}, config.GatewaySpec{})
	_, err := p.Ensure(context.Background(), "dev", config.ClusterSpec{Provider: "existing", Context: "dead-ctx"})
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != "CUBE-1101" {
		t.Fatalf("want CUBE-1101 (unreachable), got %v", err)
	}
}

func TestExistingDeleteIsNoOp(t *testing.T) {
	t.Setenv("KUBECONFIG", writeKubeconfig(t))
	p, _ := New(config.ClusterSpec{Provider: "existing", Context: "dead-ctx"}, config.GatewaySpec{})
	if err := p.Delete(context.Background(), "dev"); err != nil {
		t.Fatalf("delete must never destroy a cluster cube-idp did not create: %v", err)
	}
}
