package cluster

import (
	"bytes"
	"strings"
	"testing"
)

// installedKubeconfig builds the post-init fixture: the other-entries
// file with the cube-branded context merged in — exactly what Remove
// must reverse.
func installedKubeconfig(t *testing.T) []byte {
	t.Helper()
	const other = `apiVersion: v1
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
some-future-key:
  vendor: acme
`
	branded, err := Rebrand([]byte(kindStyleKubeconfig), "cube-idp.dev/dev", "")
	if err != nil {
		t.Fatalf("Rebrand fixture: %v", err)
	}
	merged, err := Merge([]byte(other), branded)
	if err != nil {
		t.Fatalf("Merge fixture: %v", err)
	}
	return merged
}

func TestRemove(t *testing.T) {
	removeCases := []struct {
		name        string
		existing    func(t *testing.T) []byte
		contextName string
		wantErr     bool
		wantChanged bool
		wantSubstr  []string
		notSubstr   []string
	}{
		{
			name:     "removes cube entries, keeps others and unknown keys, unsets current-context",
			existing: installedKubeconfig, contextName: "cube-idp.dev/dev",
			wantChanged: true,
			wantSubstr:  []string{"name: other", "token: abc", "vendor: acme"},
			notSubstr:   []string{"cube-idp.dev/dev", "current-context"},
		},
		{
			name:     "keeps current-context pointing at another context",
			existing: installedKubeconfig, contextName: "other",
			wantChanged: true,
			wantSubstr:  []string{"current-context: cube-idp.dev/dev"},
			notSubstr:   []string{"name: other"},
		},
		{
			name:     "absent context is a no-op returning the input bytes",
			existing: installedKubeconfig, contextName: "cube-idp.dev/nope",
			wantChanged: false,
		},
		{
			name:     "empty input is a no-op",
			existing: func(*testing.T) []byte { return nil }, contextName: "cube-idp.dev/dev",
			wantChanged: false,
		},
		{
			name: "emptied lists drop their keys entirely",
			existing: func(t *testing.T) []byte {
				t.Helper()
				branded, err := Rebrand([]byte(kindStyleKubeconfig), "cube-idp.dev/dev", "")
				if err != nil {
					t.Fatalf("Rebrand fixture: %v", err)
				}
				return branded
			},
			contextName: "cube-idp.dev/dev",
			wantChanged: true,
			wantSubstr:  []string{"apiVersion: v1"},
			notSubstr:   []string{"cube-idp.dev/dev", "clusters:", "contexts:", "users:", "current-context"},
		},
		{
			name:     "unparseable input errors",
			existing: func(*testing.T) []byte { return []byte(":\tnot yaml") }, contextName: "x",
			wantErr: true,
		},
	}

	for _, tt := range removeCases {
		t.Run(tt.name, func(t *testing.T) {
			existing := tt.existing(t)
			out, changed, err := Remove(existing, tt.contextName)
			if tt.wantErr {
				if err == nil {
					t.Fatal("want error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("Remove: %v", err)
			}
			if changed != tt.wantChanged {
				t.Fatalf("changed = %v, want %v", changed, tt.wantChanged)
			}
			if !changed && !bytes.Equal(out, existing) {
				t.Fatalf("unchanged Remove must return the input bytes verbatim:\n%s", out)
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
