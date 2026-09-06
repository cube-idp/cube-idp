// Partial-lifecycle diagnostics (issue #212): the shared fixtures and the
// create half. The delete half lives in delete_partial_test.go — one
// concern, split only to stay under the 300-line file limit.
package cluster

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"

	"github.com/cube-idp/cube-idp/internal/cubeerr"
)

// sealDir makes dir unwritable so writeKubeconfig's CreateTemp fails while
// reads still succeed — the one failure mode that reaches the atomic write
// without disturbing anything before it. The mode is restored so t.TempDir
// can clean up (cleanups run last-registered-first).
func sealDir(t *testing.T, dir string) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root writes into mode-0500 directories; the sealed case is unreachable")
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
}

// foreignKubeconfig carries entries cube-idp does not own plus the two
// top-level keys it must never understand or destroy. Both survive a
// failed cleanup and a successful retry — GOAL.md's preservation rule.
const foreignKubeconfig = `apiVersion: v1
kind: Config
preferences:
  colors: true
extensions:
  - name: someone-elses
    extension:
      keep: me
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

// topLevel returns a kubeconfig's raw top-level keys, so preservation of
// the keys cube-idp does not model is asserted semantically.
func topLevel(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := yaml.Unmarshal(raw, &m); err != nil {
		t.Fatalf("parse kubeconfig: %v", err)
	}
	return m
}

// assertForeignStateIntact checks everything that is not cube-owned came
// through untouched.
func assertForeignStateIntact(t *testing.T, raw []byte) {
	t.Helper()
	if !slicesContains(contextNames(t, raw), "other") {
		t.Errorf("the foreign context was lost:\n%s", raw)
	}
	top := topLevel(t, raw)
	for _, key := range []string{"preferences", "extensions"} {
		if _, ok := top[key]; !ok {
			t.Errorf("top-level %q was lost:\n%s", key, raw)
		}
	}
}

// contextNames parses a kubeconfig and returns its context names, so
// preservation is asserted semantically — Merge and Remove reserialise, so
// byte comparisons are only meaningful where nothing was rewritten at all.
func contextNames(t *testing.T, raw []byte) []string {
	t.Helper()
	var kc kubeconfig
	if err := yaml.Unmarshal(raw, &kc); err != nil {
		t.Fatalf("parse kubeconfig: %v", err)
	}
	names := make([]string, 0, len(kc.Contexts))
	for _, c := range kc.Contexts {
		names = append(names, c.Name)
	}
	return names
}

// TestInitPartialFailureNamesCompletedPhase: once Ensure has succeeded the
// cluster exists, and every later failure must say so. Before #212 all of
// these read "kubeconfig update failed" with no hint that the cluster is
// up, leaving an operator unable to tell "nothing happened" from "the
// cluster is running and only its context is missing".
func TestInitPartialFailureNamesCompletedPhase(t *testing.T) {
	initFailureCases := []struct {
		name     string
		mock     *mockProvisioner
		explicit bool // standalone --kubeconfig path instead of the merge path
		seal     bool // make the write fail rather than an earlier stage
	}{
		{
			name: "kubeconfig fetch fails after the cluster is ensured",
			mock: &mockProvisioner{KubeconfigFunc: func(context.Context, string) ([]byte, error) {
				return nil, errors.New("boom")
			}},
		},
		{
			name: "rebrand fails after the cluster is ensured",
			mock: &mockProvisioner{KubeconfigFunc: func(context.Context, string) ([]byte, error) {
				// Valid YAML, wrong shape: Rebrand needs exactly one entry each.
				return []byte("apiVersion: v1\nkind: Config\n"), nil
			}},
		},
		{
			name: "merge-path write fails after the cluster is ensured",
			mock: &mockProvisioner{},
			seal: true,
		},
		{
			name:     "standalone-path write fails after the cluster is ensured",
			mock:     &mockProvisioner{},
			explicit: true,
			seal:     true,
		},
	}

	for _, tt := range initFailureCases {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			opts := InitOptions{Spec: Spec{Name: "dev"}}
			t.Setenv("KUBECONFIG", filepath.Join(dir, "default-kubeconfig"))
			if tt.explicit {
				opts.KubeconfigPath = filepath.Join(dir, "explicit-kubeconfig")
			}
			if tt.seal {
				sealDir(t, dir)
			}

			err := Init(t.Context(), tt.mock, opts)

			var coded *cubeerr.Coded
			if !errors.As(err, &coded) {
				t.Fatalf("err = %v, want a *cubeerr.Coded", err)
			}
			// The code is unchanged: this is still a kubeconfig update
			// failure. Only the words gain the stage that completed.
			if coded.Code != CodeKubeconfigFailed {
				t.Fatalf("Code = %s, want %s", coded.Code, CodeKubeconfigFailed)
			}
			if !strings.Contains(coded.Summary, "cluster exists") {
				t.Errorf("Summary must state the cluster exists, got %q", coded.Summary)
			}
			if !strings.Contains(coded.Remediation, "cube-idp create") {
				t.Errorf("remediation must name the recovery command, got %q", coded.Remediation)
			}
			if coded.Unwrap() == nil {
				t.Error("the technical cause must survive the phase wording")
			}
		})
	}
}

// TestInitEnsureFailureMakesNoPhaseClaim: a failed Ensure proves nothing
// about the cluster, so the driver's error must surface untouched. This is
// the boundary the phase wording must not cross.
func TestInitEnsureFailureMakesNoPhaseClaim(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("KUBECONFIG", filepath.Join(dir, "kubeconfig"))
	mock := &mockProvisioner{EnsureFunc: func(context.Context, Spec) error {
		return NewProvisionFailedError("create", "dev", errors.New("boom"))
	}}

	err := Init(t.Context(), mock, InitOptions{Spec: Spec{Name: "dev"}})

	var coded *cubeerr.Coded
	if !errors.As(err, &coded) || coded.Code != CodeProvisionFailed {
		t.Fatalf("err = %v, want code %s", err, CodeProvisionFailed)
	}
	if strings.Contains(coded.Summary, "cluster exists") {
		t.Errorf("a failed Ensure must not claim the cluster exists: %q", coded.Summary)
	}
}

func slicesContains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
