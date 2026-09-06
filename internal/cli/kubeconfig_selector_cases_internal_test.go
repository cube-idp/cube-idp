// current-context round trip (issue #200), continued: the flag, env and
// operator-action cases. Fixtures and helpers (seededKubeconfig,
// execMerged, kubeconfigState, wantSelector, hasContext,
// wantForeignStateIntact, setupMerged) live in
// kubeconfig_selector_internal_test.go — one concern, split only to stay
// under the 300-line file limit.
package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

// TestContextNameOverrideObeysTheSameRule: with
// --kubeconfig-context-name the cube owns a name outside the
// cube-idp.dev/ prefix, and the selector rule is about the name cube-idp
// installs, not about the prefix.
func TestContextNameOverrideObeysTheSameRule(t *testing.T) {
	dir, kubeconfig := setupMerged(t, "")

	if code, _, stderr := execMerged(t, dir, "create", "--kubeconfig-context-name", "my-ctx"); code != 0 {
		t.Fatalf("create exit = %d, stderr: %s", code, stderr)
	}
	wantSelector(t, kubeconfigState(t, kubeconfig), "my-ctx")

	if code, _, stderr := execMerged(t, dir, "delete", "--kubeconfig-context-name", "my-ctx"); code != 0 {
		t.Fatalf("delete exit = %d, stderr: %s", code, stderr)
	}
	if _, present := kubeconfigState(t, kubeconfig)["current-context"]; present {
		t.Error("delete must unset the overridden name it selected")
	}
}

// TestMultiEntryKubeconfigUsesTheFirst: KUBECONFIG is a list and cube-idp
// resolves the first entry, so the selector rule applies there and the
// later entries are never opened.
func TestMultiEntryKubeconfigUsesTheFirst(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, clusterConfigYAML)
	first := filepath.Join(dir, "first")
	second := filepath.Join(dir, "second")
	if err := os.WriteFile(first, []byte(seededKubeconfig), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte(seededKubeconfig), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KUBECONFIG", first+string(os.PathListSeparator)+second)

	if code, _, stderr := execMerged(t, dir, "create"); code != 0 {
		t.Fatalf("create exit = %d, stderr: %s", code, stderr)
	}

	wantSelector(t, kubeconfigState(t, first), "other")
	if !hasContext(kubeconfigState(t, first), "cube-idp.dev/dev") {
		t.Error("the cube context was not installed into the first entry")
	}
	// Byte-for-byte against its seed: a later entry is never opened, so
	// unlike the merged file there is nothing to reserialise and exact
	// equality is the honest assertion.
	rest, err := os.ReadFile(second)
	if err != nil {
		t.Fatal(err)
	}
	if string(rest) != seededKubeconfig {
		t.Errorf("a later KUBECONFIG entry was written to:\n%s", rest)
	}
}

// TestManualCubeSelectionIsClearedByDelete pins the one consequence of
// deciding the unset by NAME rather than by provenance: an operator who
// selects the cube themselves after create finds that selection cleared
// by delete. Nothing records who selected a context, so provenance is not
// available to decide on — and the rule is sound anyway, because the
// context the selector names is removed in the same call, so the
// selection could not survive it whoever made it. Documented in
// docs/domains/cluster.md; pinned here so it cannot drift silently.
func TestManualCubeSelectionIsClearedByDelete(t *testing.T) {
	dir, kubeconfig := setupMerged(t, seededKubeconfig)

	if code, _, stderr := execMerged(t, dir, "create"); code != 0 {
		t.Fatalf("create exit = %d, stderr: %s", code, stderr)
	}
	// The operator's own `kubectl config use-context cube-idp.dev/dev`.
	state := kubeconfigState(t, kubeconfig)
	state["current-context"] = "cube-idp.dev/dev"
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

	after := kubeconfigState(t, kubeconfig)
	if _, present := after["current-context"]; present {
		t.Error("a selector naming the removed context must not be left dangling")
	}
	// The rest of the operator's file is untouched, which is the half the
	// decision actually guarantees.
	wantForeignStateIntact(t, after)
}

// TestStandalonePathReplacesTheFileAndSelectsTheCube drives the real
// --kubeconfig path — not Rebrand in isolation — against a file that
// already has entries and a foreign selection. It pins the operator's D1
// answer (the cube is always selected there) together with the wholesale
// replacement that makes that answer the right one: there is no other
// selection left in the file to preserve.
func TestStandalonePathReplacesTheFileAndSelectsTheCube(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeConfig(t, dir, clusterConfigYAML)
	kubeconfig := filepath.Join(dir, "kubeconfig")
	if err := os.WriteFile(kubeconfig, []byte(seededKubeconfig), 0o600); err != nil {
		t.Fatal(err)
	}

	if code, _, stderr := execCreate(t, dir); code != 0 {
		t.Fatalf("create exit = %d, stderr: %s", code, stderr)
	}

	state := kubeconfigState(t, kubeconfig)
	wantSelector(t, state, "cube-idp.dev/dev")
	if !hasContext(state, "cube-idp.dev/dev") {
		t.Error("the cube context was not installed")
	}
	// Wholesale replacement, which README and docs/domains/cluster.md both
	// document: nothing of the previous file remains.
	if hasContext(state, "other") {
		t.Error("--kubeconfig must replace the file wholesale, not merge")
	}
	if _, ok := state["some-future-key"]; ok {
		t.Error("--kubeconfig must replace the file wholesale, not merge")
	}
}

// TestTwoCubesDeletedInEitherOrder: with two cubes in one kubeconfig the
// selector rule has to hold whichever is removed first. The first create
// selects its cube because nothing was selected; the second does not
// displace it; and only the delete that removes the *selected* cube
// clears the selector.
func TestTwoCubesDeletedInEitherOrder(t *testing.T) {
	orders := []struct {
		name  string
		first string // deleted first
		then  string
	}{
		{name: "selected cube deleted first", first: "one", then: "two"},
		{name: "selected cube deleted last", first: "two", then: "one"},
	}

	for _, tt := range orders {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			kubeconfig := filepath.Join(dir, "kubeconfig")
			t.Setenv("KUBECONFIG", kubeconfig)
			for _, cube := range []string{"one", "two"} {
				sub := filepath.Join(dir, cube)
				if err := os.MkdirAll(sub, 0o700); err != nil {
					t.Fatal(err)
				}
				writeConfig(t, sub, strings.ReplaceAll(clusterConfigYAML, "name: dev", "name: "+cube))
				if code, _, stderr := execMerged(t, sub, "create"); code != 0 {
					t.Fatalf("create %s exit = %d, stderr: %s", cube, code, stderr)
				}
			}
			// "one" was created first into an empty file, so it holds the
			// selection; "two" did not displace it.
			wantSelector(t, kubeconfigState(t, kubeconfig), "cube-idp.dev/one")

			if code, _, stderr := execMerged(t, filepath.Join(dir, tt.first), "delete"); code != 0 {
				t.Fatalf("delete %s exit = %d, stderr: %s", tt.first, code, stderr)
			}
			state := kubeconfigState(t, kubeconfig)
			if tt.first == "one" {
				if _, present := state["current-context"]; present {
					t.Error("deleting the selected cube must clear the selector")
				}
			} else {
				wantSelector(t, state, "cube-idp.dev/one")
				if !hasContext(state, "cube-idp.dev/one") {
					t.Error("the surviving cube's context was removed")
				}
			}

			if code, _, stderr := execMerged(t, filepath.Join(dir, tt.then), "delete"); code != 0 {
				t.Fatalf("delete %s exit = %d, stderr: %s", tt.then, code, stderr)
			}
			final := kubeconfigState(t, kubeconfig)
			if _, present := final["current-context"]; present {
				t.Error("no cube is left, so no cube-owned selection may remain")
			}
			for _, cube := range []string{"one", "two"} {
				if hasContext(final, "cube-idp.dev/"+cube) {
					t.Errorf("cube %q context survived its delete", cube)
				}
			}
		})
	}
}
