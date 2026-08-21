package pack

import (
	"testing"
)

// The grammar is small and load-bearing, so it is pinned directly rather than
// only through a render.
func TestExpand(t *testing.T) {
	vars := map[string]string{"IMAGE": "nginx:1.27", "TIER": "backend", "EMPTY": ""}

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"no reference is untouched", "plain text", "plain text"},
		{"whole scalar", "${IMAGE}", "nginx:1.27"},
		{"reference inside text", "image is ${IMAGE} today", "image is nginx:1.27 today"},
		{"several references", "${TIER}/${IMAGE}", "backend/nginx:1.27"},
		{"repeated reference", "${TIER}-${TIER}", "backend-backend"},
		{"an empty value substitutes as empty", "[${EMPTY}]", "[]"},

		{"escape yields a literal reference", "$${IMAGE}", "${IMAGE}"},
		{"escape beside a real reference", "$${IMAGE} ${IMAGE}", "${IMAGE} nginx:1.27"},

		{"a dollar not opening a reference is literal", "costs $5", "costs $5"},
		{"a trailing dollar is literal", "ends with $", "ends with $"},
		{"a dollar before a letter is literal", "$IMAGE", "$IMAGE"},
		{"the shell brace-less form is not a reference", "$IMAGE:$TIER", "$IMAGE:$TIER"},

		{"an unterminated reference is literal", "${IMAGE", "${IMAGE"},
		{"an empty name is not a reference", "${}", "${}"},
		{"a name starting with a digit is not a reference", "${1BAD}", "${1BAD}"},
		{"a name with a dash is not a reference", "${NOT-A-NAME}", "${NOT-A-NAME}"},
		{"a name with a space is not a reference", "${NOT A NAME}", "${NOT A NAME}"},

		// There are no shell-style defaults: #Values defaults are the one
		// defaulting mechanism, so this is not a reference at all.
		{"shell-style default is not a reference", "${IMAGE:-fallback}", "${IMAGE:-fallback}"},
		{"shell-style assign default is not a reference", "${IMAGE:=fallback}", "${IMAGE:=fallback}"},

		{"underscore names are valid", "${TIER}", "backend"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			missing := map[string]struct{}{}
			if got := expand(tt.in, vars, missing); got != tt.want {
				t.Errorf("expand(%q) = %q, want %q", tt.in, got, tt.want)
			}
			if len(missing) != 0 {
				t.Errorf("expand(%q) reported missing %v, want none", tt.in, sortedKeys(missing))
			}
		})
	}
}

// Every unresolved variable is collected, so one run names all of them.
func TestExpandCollectsMissing(t *testing.T) {
	missing := map[string]struct{}{}
	expand("${A} and ${B} and ${A}", map[string]string{}, missing)

	got := sortedKeys(missing)
	if len(got) != 2 || got[0] != "A" || got[1] != "B" {
		t.Errorf("expand() missing = %v, want [A B]", got)
	}
}

// An escaped reference must not count as a use, or escaping would demand a
// value for something deliberately kept literal.
func TestExpandEscapeIsNotAUse(t *testing.T) {
	missing := map[string]struct{}{}
	if got := expand("$${NOPE}", map[string]string{}, missing); got != "${NOPE}" {
		t.Errorf("expand($${NOPE}) = %q, want ${NOPE}", got)
	}
	if len(missing) != 0 {
		t.Errorf("escaped reference reported missing %v, want none", sortedKeys(missing))
	}
}

// Names are [A-Za-z_][A-Za-z0-9_]* — nothing wider, so the grammar cannot
// drift into shell syntax by accident.
func TestIsVarName(t *testing.T) {
	valid := []string{"A", "_", "a", "IMAGE", "image_tag", "_private", "A1", "x9_y"}
	invalid := []string{"", "1", "1A", "a-b", "a.b", "a b", "a/b", "a:b", "${x}", "a$"}

	for _, name := range valid {
		if !isVarName(name) {
			t.Errorf("isVarName(%q) = false, want true", name)
		}
	}
	for _, name := range invalid {
		if isVarName(name) {
			t.Errorf("isVarName(%q) = true, want false", name)
		}
	}
}

// The scan reimplements kustomize's unexported heuristic, so both directions
// are pinned: remote must never slip through, and ordinary local paths must
// keep working.
func TestIsRemoteRef(t *testing.T) {
	remote := []string{
		"https://example.com/cm.yaml",
		"http://example.com/cm.yaml",
		"ssh://git@example.com/org/repo",
		"oci://registry.example/base:1.0",
		"git@github.com:org/repo.git",
		"git::https://example.com/org/repo",
		"github.com/org/repo",
		"github.com/org/repo//overlays/prod",
		"gitlab.com/org/repo?ref=main",
		"bitbucket.org/org/repo",
		"example.com/repo.git",
	}
	local := []string{
		"",
		"cm.yaml",
		"./cm.yaml",
		"../shared/cm.yaml",
		"/abs/cm.yaml",
		"base",
		"base/cm.yaml",
		"overlays/prod",
		"nested/deep/cm.yml",
		"a.yaml",
	}

	for _, ref := range remote {
		if !isRemoteRef(ref) {
			t.Errorf("isRemoteRef(%q) = false, want true — a remote reference must never slip through", ref)
		}
	}
	for _, ref := range local {
		if isRemoteRef(ref) {
			t.Errorf("isRemoteRef(%q) = true, want false — a local path must not be rejected", ref)
		}
	}
}
