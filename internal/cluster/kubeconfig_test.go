package cluster

import (
	"strings"
	"testing"
)

const kindStyleKubeconfig = `apiVersion: v1
kind: Config
clusters:
  - name: kind-dev
    cluster:
      server: https://127.0.0.1:52556
      certificate-authority-data: Zm9v
contexts:
  - name: kind-dev
    context:
      cluster: kind-dev
      user: kind-dev
users:
  - name: kind-dev
    user:
      client-certificate-data: YmFy
current-context: kind-dev
`

func TestContextName(t *testing.T) {
	if got, want := ContextName("dev"), "cube-idp.dev/dev"; got != want {
		t.Fatalf("ContextName = %q, want %q", got, want)
	}
}

func TestRebrand(t *testing.T) {
	rebrandCases := []struct {
		name        string
		raw         string
		contextName string
		namespace   string
		wantErr     bool
		wantSubstr  []string
		notSubstr   []string
	}{
		{
			name: "renames all entries and current-context",
			raw:  kindStyleKubeconfig, contextName: "cube-idp.dev/dev",
			wantSubstr: []string{"cube-idp.dev/dev", "certificate-authority-data: Zm9v", "client-certificate-data: YmFy"},
			notSubstr:  []string{"kind-dev"},
		},
		{
			name: "stamps namespace when set",
			raw:  kindStyleKubeconfig, contextName: "cube-idp.dev/dev", namespace: "platform",
			wantSubstr: []string{"namespace: platform"},
		},
		{
			name: "omits namespace when empty",
			raw:  kindStyleKubeconfig, contextName: "cube-idp.dev/dev",
			notSubstr: []string{"namespace:"},
		},
		{
			name: "rejects multi-context kubeconfig",
			raw: kindStyleKubeconfig + `  - name: other
    context:
      cluster: other
      user: other
`,
			contextName: "x", wantErr: true,
		},
		{name: "rejects unparseable input", raw: ":\tnot yaml", contextName: "x", wantErr: true},
	}

	for _, tt := range rebrandCases {
		t.Run(tt.name, func(t *testing.T) {
			out, err := Rebrand([]byte(tt.raw), tt.contextName, tt.namespace)
			if tt.wantErr {
				if err == nil {
					t.Fatal("want error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("Rebrand: %v", err)
			}
			for _, s := range tt.wantSubstr {
				if !strings.Contains(string(out), s) {
					t.Errorf("output missing %q:\n%s", s, out)
				}
			}
			for _, s := range tt.notSubstr {
				if strings.Contains(string(out), s) {
					t.Errorf("output still contains %q:\n%s", s, out)
				}
			}
		})
	}
}

func TestMerge(t *testing.T) {
	// otherKubeconfig is the pre-existing-file fixture.
	const otherKubeconfig = `apiVersion: v1
kind: Config
clusters:
  - name: other
    cluster:
      server: https://example.com
contexts:
  - name: other
    context:
      cluster: other
      user: other
users:
  - name: other
    user:
      token: abc
current-context: other
`

	branded, err := Rebrand([]byte(kindStyleKubeconfig), "cube-idp.dev/dev", "")
	if err != nil {
		t.Fatalf("Rebrand fixture: %v", err)
	}
	tests := []struct {
		name       string
		existing   string
		wantSubstr []string
	}{
		{
			name:       "into empty file yields incoming",
			wantSubstr: []string{"cube-idp.dev/dev", "current-context: cube-idp.dev/dev"},
		},
		{
			name:       "preserves other entries, upserts ours, takes current-context",
			existing:   otherKubeconfig,
			wantSubstr: []string{"name: other", "token: abc", "cube-idp.dev/dev", "current-context: cube-idp.dev/dev"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := Merge([]byte(tt.existing), branded)
			if err != nil {
				t.Fatalf("Merge: %v", err)
			}
			for _, s := range tt.wantSubstr {
				if !strings.Contains(string(out), s) {
					t.Errorf("output missing %q:\n%s", s, out)
				}
			}
		})
	}
	// merging the same incoming twice must not duplicate entries
	once, _ := Merge([]byte(otherKubeconfig), branded)
	twice, err := Merge(once, branded)
	if err != nil {
		t.Fatalf("Merge (idempotent): %v", err)
	}
	if strings.Count(string(twice), "name: cube-idp.dev/dev") != strings.Count(string(once), "name: cube-idp.dev/dev") {
		t.Fatal("Merge duplicated entries on re-merge")
	}
}
