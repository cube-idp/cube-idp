// The operator-visible current-context round trip (issue #200).
//
// These tests drive the MERGE path deliberately: KUBECONFIG is set and no
// --kubeconfig flag is passed. Both existing helpers (execCreate,
// execDelete) pass --kubeconfig, which is the STANDALONE path — it
// replaces the file wholesale, so every preservation assertion made
// through them would pass vacuously against a file cube-idp had just
// rewritten.
package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"sigs.k8s.io/yaml"

	v1alpha1 "github.com/cube-idp/cube-idp/api/config/v1alpha1"
	"github.com/cube-idp/cube-idp/internal/cluster"
)

// seededKubeconfig is a user's file: a foreign context they are working
// in, a selection naming it, and two top-level keys cube-idp must never
// destroy.
const seededKubeconfig = `apiVersion: v1
kind: Config
preferences:
  colors: true
some-future-key:
  vendor: acme
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

// execMerged runs a verb against the default kubeconfig location — no
// --kubeconfig — so the merge path is exercised.
func execMerged(t *testing.T, dir, verb string, extra ...string) (code int, stdout, stderr string) {
	t.Helper()
	root := newRootCmd(func(v1alpha1.ClusterProvider) (cluster.Provisioner, error) {
		return mockProvisioner{}, nil
	}, defaultEngine)
	var out, errBuf bytes.Buffer
	args := append([]string{verb, "-f", filepath.Join(dir, "cube.yaml")}, extra...)
	code = execute(t.Context(), root, args, &out, &errBuf)
	return code, out.String(), errBuf.String()
}

// kubeconfigState is the parsed view every assertion here uses. Merge and
// Remove reserialise, so byte comparisons would be meaningless.
func kubeconfigState(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read kubeconfig: %v", err)
	}
	var m map[string]any
	if err := yaml.Unmarshal(raw, &m); err != nil {
		t.Fatalf("parse kubeconfig %s: %v", path, err)
	}
	return m
}

func wantSelector(t *testing.T, state map[string]any, want string) {
	t.Helper()
	got, _ := state["current-context"].(string)
	if got != want {
		t.Errorf("current-context = %q, want %q", got, want)
	}
}

func hasContext(state map[string]any, name string) bool {
	list, _ := state["contexts"].([]any)
	for _, e := range list {
		m, _ := e.(map[string]any)
		if n, _ := m["name"].(string); n == name {
			return true
		}
	}
	return false
}

// wantForeignStateIntact compares the operator's own state against the
// seed by VALUE, not by mere presence: a key that survives with mangled
// contents is just as much a loss as one that vanishes. The comparison is
// semantic — Merge and Remove reserialise, so the bytes legitimately
// differ.
func wantForeignStateIntact(t *testing.T, state map[string]any) {
	t.Helper()
	var seed map[string]any
	if err := yaml.Unmarshal([]byte(seededKubeconfig), &seed); err != nil {
		t.Fatalf("parse seed: %v", err)
	}
	for _, key := range []string{"preferences", "some-future-key"} {
		if !reflect.DeepEqual(state[key], seed[key]) {
			t.Errorf("top-level %q changed:\n got %#v\nwant %#v", key, state[key], seed[key])
		}
	}
	for _, list := range []string{"clusters", "contexts", "users"} {
		want := namedEntry(t, seed, list, "other")
		got := namedEntry(t, state, list, "other")
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s entry %q changed:\n got %#v\nwant %#v", list, "other", got, want)
		}
	}
}

// namedEntry pulls one entry out of a kubeconfig list by name.
func namedEntry(t *testing.T, state map[string]any, list, name string) map[string]any {
	t.Helper()
	entries, _ := state[list].([]any)
	for _, e := range entries {
		m, _ := e.(map[string]any)
		if n, _ := m["name"].(string); n == name {
			return m
		}
	}
	return nil
}

// setupMerged writes the config and seeds the default kubeconfig, and
// returns its path. t.Setenv forbids t.Parallel, so nothing here is
// parallel — the environment is the fixture.
func setupMerged(t *testing.T, seed string) (dir, kubeconfig string) {
	t.Helper()
	dir = t.TempDir()
	writeConfig(t, dir, clusterConfigYAML)
	kubeconfig = filepath.Join(dir, "kubeconfig")
	if seed != "" {
		if err := os.WriteFile(kubeconfig, []byte(seed), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("KUBECONFIG", kubeconfig)
	return dir, kubeconfig
}

// TestCreateDoesNotDisplaceTheOperatorsSelection is the reported defect,
// end to end: an operator working in "other" runs create and finds
// kubectl still pointing at "other".
func TestCreateDoesNotDisplaceTheOperatorsSelection(t *testing.T) {
	dir, kubeconfig := setupMerged(t, seededKubeconfig)

	if code, _, stderr := execMerged(t, dir, "create"); code != 0 {
		t.Fatalf("create exit = %d, stderr: %s", code, stderr)
	}

	state := kubeconfigState(t, kubeconfig)
	wantSelector(t, state, "other")
	if !hasContext(state, "cube-idp.dev/dev") {
		t.Error("the cube context was not installed")
	}
	wantForeignStateIntact(t, state)
}

// TestCreateDeleteRoundTripPreservesTheSelection: the whole lifecycle
// leaves the operator's selection exactly as they left it, and takes only
// the cube's own entries back out.
func TestCreateDeleteRoundTripPreservesTheSelection(t *testing.T) {
	dir, kubeconfig := setupMerged(t, seededKubeconfig)

	if code, _, stderr := execMerged(t, dir, "create"); code != 0 {
		t.Fatalf("create exit = %d, stderr: %s", code, stderr)
	}
	if code, _, stderr := execMerged(t, dir, "delete"); code != 0 {
		t.Fatalf("delete exit = %d, stderr: %s", code, stderr)
	}

	state := kubeconfigState(t, kubeconfig)
	wantSelector(t, state, "other")
	if hasContext(state, "cube-idp.dev/dev") {
		t.Error("the cube context survived delete")
	}
	wantForeignStateIntact(t, state)
}

// TestCreateSelectsTheCubeWhenNothingIsSelected: nothing is displaced, so
// a first cube on a fresh kubeconfig still just works — and delete unsets
// exactly what create set.
func TestCreateSelectsTheCubeWhenNothingIsSelected(t *testing.T) {
	dir, kubeconfig := setupMerged(t, "")

	if code, _, stderr := execMerged(t, dir, "create"); code != 0 {
		t.Fatalf("create exit = %d, stderr: %s", code, stderr)
	}
	wantSelector(t, kubeconfigState(t, kubeconfig), "cube-idp.dev/dev")

	if code, _, stderr := execMerged(t, dir, "delete"); code != 0 {
		t.Fatalf("delete exit = %d, stderr: %s", code, stderr)
	}
	if _, present := kubeconfigState(t, kubeconfig)["current-context"]; present {
		t.Error("delete must unset the selection it set")
	}
}

// TestOperatorSelectionBetweenCreateAndDeleteSurvives: create selected the
// cube, the operator then chose something else, and delete must respect
// that later choice rather than the one it made.
func TestOperatorSelectionBetweenCreateAndDeleteSurvives(t *testing.T) {
	// Seeded, so "other" is a context that actually exists: selecting a
	// name with no entry behind it would not be a realistic operator
	// action and would make the assertion weaker than it looks.
	dir, kubeconfig := setupMerged(t, seededKubeconfig)

	if code, _, stderr := execMerged(t, dir, "create"); code != 0 {
		t.Fatalf("create exit = %d, stderr: %s", code, stderr)
	}
	// Stand in for `kubectl config use-context other`.
	state := kubeconfigState(t, kubeconfig)
	state["current-context"] = "other"
	raw, err := yaml.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(kubeconfig, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	if code, _, stderr := execMerged(t, dir, "delete"); code != 0 {
		t.Fatalf("delete exit = %d, stderr: %s", code, stderr)
	}
	wantSelector(t, kubeconfigState(t, kubeconfig), "other")
}

// TestRepeatedCreateLeavesTheSelectionAlone: create is re-runnable, and a
// second run must not quietly re-take a selection the first one left.
func TestRepeatedCreateLeavesTheSelectionAlone(t *testing.T) {
	dir, kubeconfig := setupMerged(t, seededKubeconfig)

	for i := range 2 {
		if code, _, stderr := execMerged(t, dir, "create"); code != 0 {
			t.Fatalf("create %d exit = %d, stderr: %s", i, code, stderr)
		}
	}
	wantSelector(t, kubeconfigState(t, kubeconfig), "other")
}
