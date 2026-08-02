// White-box test (package config): the wordlists are deliberately
// unexported, and the exhaustive cross-product assertion below is what
// keeps them safe to edit.
package config

import (
	"slices"
	"strings"
	"testing"

	v1alpha1 "github.com/cube-idp/cube-idp/api/config/v1alpha1"
)

// assertValidCubeName runs a name through the real api validation so the
// wordlists can never drift from the name regex.
func assertValidCubeName(t *testing.T, name string) {
	t.Helper()
	var c v1alpha1.Config
	c.Name = name
	if errs := c.Validate(); len(errs) > 0 {
		t.Errorf("name %q fails api validation: %v", name, errs.ToAggregate())
	}
}

func TestNameWordlistsEveryCombinationIsValid(t *testing.T) {
	if len(nameAdjectives) == 0 || len(nameNouns) == 0 {
		t.Fatal("wordlists must not be empty")
	}
	for _, adj := range nameAdjectives {
		for _, noun := range nameNouns {
			assertValidCubeName(t, adj+"-"+noun)
		}
	}
}

func TestGenerateName(t *testing.T) {
	for range 100 {
		name := GenerateName()
		// Adjectives contain no dash, so the first dash is the separator.
		adj, noun, ok := strings.Cut(name, "-")
		if !ok {
			t.Fatalf("name %q is not <adjective>-<noun>", name)
		}
		if !slices.Contains(nameAdjectives, adj) {
			t.Errorf("adjective %q of %q is not from the wordlist", adj, name)
		}
		if !slices.Contains(nameNouns, noun) {
			t.Errorf("noun %q of %q is not from the wordlist", noun, name)
		}
		assertValidCubeName(t, name)
	}
}
