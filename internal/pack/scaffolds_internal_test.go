package pack

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/cube-idp/cube-idp/internal/cubeerr"
)

// The unsupported-type refusal names every type this build can scaffold.
//
// Deriving the expectation from the scaffolds map rather than from a literal
// list is the point: a type added there without touching the remediation fails
// here instead of shipping a message that omits it. That is exactly how
// CUBE-PKG-023 came to offer raw and kustomize after helm became scaffoldable
// (docs/domains/pack.md:946 — "Every `type` is scaffoldable from M9 on").
func TestScaffoldTypeUnsupportedRemediationNamesEveryScaffoldableType(t *testing.T) {
	err := newScaffoldTypeUnsupportedError(Type("bogus"))

	var coded *cubeerr.Coded
	if !errors.As(err, &coded) {
		t.Fatalf("error %v is not a *cubeerr.Coded", err)
	}
	if coded.Code != CodeScaffoldFailed {
		t.Fatalf("error code = %s, want %s", coded.Code, CodeScaffoldFailed)
	}

	// The whole list, joined as the message joins it, rather than one
	// Contains per type: that pins membership *and* order in a single
	// assertion, so dropping the sort in scaffoldableTypes fails here instead
	// of shipping a remediation that reorders itself between runs.
	names := make([]string, 0, len(scaffolds))
	for packType := range scaffolds {
		names = append(names, string(packType))
	}
	slices.Sort(names)
	want := strings.Join(names, ", ")
	if !strings.Contains(coded.Remediation, want) {
		t.Errorf("remediation %q does not name every scaffoldable type in sorted order (%q)",
			coded.Remediation, want)
	}
}
