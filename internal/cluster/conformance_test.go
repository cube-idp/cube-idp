package cluster

import (
	"context"
	"fmt"
	"testing"
)

// fakeProvisioner is the hand-rolled stateful reference implementation:
// it proves the conformance suite itself without needing Docker.
type fakeProvisioner struct {
	clusters map[string]bool
}

func newFake() *fakeProvisioner { return &fakeProvisioner{clusters: map[string]bool{}} }

func (f *fakeProvisioner) Ensure(_ context.Context, s Spec) error {
	f.clusters[s.Name] = true
	return nil
}

func (f *fakeProvisioner) Exists(_ context.Context, name string) (bool, error) {
	return f.clusters[name], nil
}

func (f *fakeProvisioner) Delete(_ context.Context, name string) error {
	delete(f.clusters, name)
	return nil
}

func (f *fakeProvisioner) Kubeconfig(_ context.Context, name string) ([]byte, error) {
	if !f.clusters[name] {
		return nil, fmt.Errorf("cluster %q not found", name)
	}
	return []byte(fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
  - name: fake-%[1]s
    cluster:
      server: https://127.0.0.1:6443
contexts:
  - name: fake-%[1]s
    context:
      cluster: fake-%[1]s
      user: fake-%[1]s
users:
  - name: fake-%[1]s
    user:
      token: fake
current-context: fake-%[1]s
`, name)), nil
}

func TestConformance_Fake(t *testing.T) {
	RunClusterConformance(t, func() Provisioner { return newFake() })
}
