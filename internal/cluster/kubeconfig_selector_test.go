// current-context ownership (issue #200). The selector is the one piece
// of a kubeconfig that is global rather than per-entry, so the rule is
// stated and tested on its own: cube-idp selects its context only when
// nothing is selected, and unsets only a selection that names its own
// context. Assertions parse the YAML — "current-context" as a substring
// also matches `current-context: other`, which would make every negative
// check here unreliable.
package cluster

import (
	"testing"

	"sigs.k8s.io/yaml"
)

// selector returns the kubeconfig's current-context, and whether the key
// is present at all — absent and empty are different states and the
// policy distinguishes them.
func selector(t *testing.T, raw []byte) (value string, present bool) {
	t.Helper()
	var m map[string]any
	if err := yaml.Unmarshal(raw, &m); err != nil {
		t.Fatalf("parse kubeconfig: %v", err)
	}
	v, ok := m["current-context"]
	if !ok {
		return "", false
	}
	s, _ := v.(string)
	return s, true
}

func brandedFixture(t *testing.T) []byte {
	t.Helper()
	branded, err := Rebrand([]byte(kindStyleKubeconfig), "cube-idp.dev/dev", "")
	if err != nil {
		t.Fatalf("Rebrand fixture: %v", err)
	}
	return branded
}

// TestMergeNeverDisplacesAnExistingSelection is issue #200's create half.
// Merge used to adopt the incoming selector unconditionally, so every
// `create` retargeted a live kubectl at the new cube — the half of the
// defect that was documented nowhere.
func TestMergeNeverDisplacesAnExistingSelection(t *testing.T) {
	t.Parallel()

	const withForeign = `apiVersion: v1
kind: Config
contexts:
  - name: other
    context:
      cluster: other
      user: other
current-context: other
`
	const withoutSelector = `apiVersion: v1
kind: Config
contexts:
  - name: other
    context:
      cluster: other
      user: other
`
	const withEmptySelector = `apiVersion: v1
kind: Config
contexts:
  - name: other
    context:
      cluster: other
      user: other
current-context: ""
`
	const alreadyOurs = `apiVersion: v1
kind: Config
contexts:
  - name: cube-idp.dev/dev
    context:
      cluster: cube-idp.dev/dev
      user: cube-idp.dev/dev
current-context: cube-idp.dev/dev
`

	selectorCases := []struct {
		name        string
		existing    string
		wantValue   string
		wantPresent bool
	}{
		{
			// The defect: an operator working in `other` ran `create` and
			// found kubectl pointing at the cube.
			name:      "an operator's selection survives",
			existing:  withForeign,
			wantValue: "other", wantPresent: true,
		},
		{
			// Nothing is displaced, so the first cube still just works.
			name:      "no selection means the cube is selected",
			existing:  withoutSelector,
			wantValue: "cube-idp.dev/dev", wantPresent: true,
		},
		{
			// Present-but-empty is not a selection.
			name:      "an empty selection counts as none",
			existing:  withEmptySelector,
			wantValue: "cube-idp.dev/dev", wantPresent: true,
		},
		{
			name:      "an empty file yields the incoming selection",
			existing:  "",
			wantValue: "cube-idp.dev/dev", wantPresent: true,
		},
		{
			// Re-running create over our own selection changes nothing.
			name:      "a selection already naming the cube is idempotent",
			existing:  alreadyOurs,
			wantValue: "cube-idp.dev/dev", wantPresent: true,
		},
	}

	for _, tt := range selectorCases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			out, err := Merge([]byte(tt.existing), brandedFixture(t))
			if err != nil {
				t.Fatalf("Merge: %v", err)
			}
			got, present := selector(t, out)
			if present != tt.wantPresent {
				t.Fatalf("current-context present = %v, want %v:\n%s", present, tt.wantPresent, out)
			}
			if got != tt.wantValue {
				t.Errorf("current-context = %q, want %q:\n%s", got, tt.wantValue, out)
			}
		})
	}
}

// TestRemoveUnsetsOnlyItsOwnSelection is the delete half. Paired with the
// Merge rule above it gives the symmetry issue #200 asks for: delete can
// only clear a selection that create could have set.
func TestRemoveUnsetsOnlyItsOwnSelection(t *testing.T) {
	t.Parallel()

	// A file where the operator had a selection: create preserved it, so
	// delete must too.
	foreignSelected, err := Merge([]byte(`apiVersion: v1
kind: Config
contexts:
  - name: other
    context:
      cluster: other
      user: other
current-context: other
`), brandedFixture(t))
	if err != nil {
		t.Fatalf("Merge fixture: %v", err)
	}
	// A file where the operator had none: create selected the cube, so
	// delete unsets exactly what it set.
	cubeSelected, err := Merge([]byte(`apiVersion: v1
kind: Config
contexts:
  - name: other
    context:
      cluster: other
      user: other
`), brandedFixture(t))
	if err != nil {
		t.Fatalf("Merge fixture: %v", err)
	}

	t.Run("a foreign selection survives the delete", func(t *testing.T) {
		t.Parallel()
		out, changed, err := Remove(foreignSelected, "cube-idp.dev/dev")
		if err != nil || !changed {
			t.Fatalf("Remove = (changed %v, %v), want (true, nil)", changed, err)
		}
		got, present := selector(t, out)
		if !present || got != "other" {
			t.Errorf("current-context = (%q, present %v), want (\"other\", true):\n%s", got, present, out)
		}
	})

	t.Run("the cube's own selection is unset", func(t *testing.T) {
		t.Parallel()
		out, changed, err := Remove(cubeSelected, "cube-idp.dev/dev")
		if err != nil || !changed {
			t.Fatalf("Remove = (changed %v, %v), want (true, nil)", changed, err)
		}
		if _, present := selector(t, out); present {
			t.Errorf("current-context must be unset when it named the removed context:\n%s", out)
		}
	})
}

// TestStandalonePathSelectsTheCube pins the one thing this package owns
// on the --kubeconfig path: Rebrand stamps the selector, and those bytes
// are what gets written, unmerged. The path itself — that it replaces the
// file wholesale, so there is no surviving selection to protect and the
// cube is therefore always selected — is exercised end to end by
// TestStandalonePathReplacesTheFileAndSelectsTheCube in internal/cli,
// because writeKubeconfig is chosen at the operation level, not here.
func TestStandalonePathSelectsTheCube(t *testing.T) {
	t.Parallel()
	got, present := selector(t, brandedFixture(t))
	if !present || got != "cube-idp.dev/dev" {
		t.Errorf("current-context = (%q, present %v), want (\"cube-idp.dev/dev\", true)", got, present)
	}
}
