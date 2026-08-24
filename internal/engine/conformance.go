package engine

import (
	"errors"
	"fmt"
	"reflect"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	v1alpha1 "github.com/cube-idp/cube-idp/api/config/v1alpha1"
	"github.com/cube-idp/cube-idp/internal/cubeerr"
)

// RunEngineConformance asserts the behavioral contract every Provider
// must satisfy, against driver-supplied fixtures: no source yields no
// sync wiring; a configured source yields exactly the fixture's expected
// objects, every one recognized by the driver's own Reconciled;
// EngineObjects is empty (the degenerate case) or a bundle in which
// EngineNamespace names a Namespace object; EngineNamespace is
// non-empty; Reconciled judges ready, not-ready, stale, and
// unknown-status fixtures correctly — no stale success may count as
// reconciled — and yields the coded error for unrecognized objects; the
// optional SpecValidator validates driver-owned fixtures. The seam is
// pure, so the suite runs hermetically against the real driver; drivers
// run it from their own test packages.
func RunEngineConformance(t *testing.T, factory func() (Provider, Fixtures)) {
	t.Helper()
	p, fx := factory()
	if ns := p.EngineNamespace(); ns == "" {
		t.Fatal(`EngineNamespace() = "", want a non-empty namespace`)
	}
	assertNoSource(t, p, fx.NoSource)
	assertSources(t, p, fx.Sources)
	assertEngineBundle(t, p, fx.NoSource, "NoSource")
	for _, s := range fx.Sources {
		assertEngineBundle(t, p, s.Spec, s.Name)
	}
	assertStatusJudgments(t, p, fx.Statuses)
	assertSpecValidation(t, p, fx)
}

// assertNoSource checks the documented nil-source semantic: a spec
// without a source yields no sync wiring and no error.
func assertNoSource(t *testing.T, p Provider, spec v1alpha1.EngineSpec) {
	t.Helper()
	if spec.Source != nil {
		t.Fatal("fixture NoSource must not configure spec.source")
	}
	objs, err := p.SourceObjects(t.Context(), spec)
	if err != nil {
		t.Fatalf("SourceObjects (no source): %v", err)
	}
	if len(objs) != 0 {
		t.Fatalf("SourceObjects (no source) = %d objects, want none", len(objs))
	}
}

// assertSources checks each configured-source fixture yields exactly its
// expected objects — the fixture's statement of the git|oci contract,
// not merely a non-empty slice — all declared and recognized.
func assertSources(t *testing.T, p Provider, sources []SourceFixture) {
	t.Helper()
	if len(sources) == 0 {
		t.Fatal("fixtures must supply at least one configured-source spec")
	}
	for _, s := range sources {
		if s.Spec.Source == nil {
			t.Fatalf("source fixture %q must configure spec.source", s.Name)
		}
		if len(s.Want) == 0 {
			t.Fatalf("source fixture %q must declare its expected objects", s.Name)
		}
		objs, err := p.SourceObjects(t.Context(), s.Spec)
		if err != nil {
			t.Fatalf("SourceObjects(%s): %v", s.Name, err)
		}
		assertDeclaredObjects(t, p, objs, "SourceObjects("+s.Name+")")
		assertObjectsMatch(t, objs, s.Want, "SourceObjects("+s.Name+")")
	}
}

// assertObjectsMatch checks got is exactly the expected objects, matched
// by identity in any order (the seam documents no ordering) and compared
// deeply — identity alone would let a driver emit correctly named
// objects whose source-derived content (url, ref, path) ignores the
// spec.
func assertObjectsMatch(t *testing.T, got, want []*unstructured.Unstructured, from string) {
	t.Helper()
	byID := make(map[string]*unstructured.Unstructured, len(want))
	for i, w := range want {
		if w == nil {
			t.Fatalf("%s expected object %d is nil", from, i)
		}
		byID[identityKey(w)] = w
	}
	if len(byID) != len(want) {
		t.Fatalf("%s fixture declares duplicate expected identities", from)
	}
	if len(got) != len(want) {
		t.Fatalf("%s = %d objects, want %d", from, len(got), len(want))
	}
	for _, g := range got {
		w, ok := byID[identityKey(g)]
		if !ok {
			t.Fatalf("%s declared unexpected object %s", from, identityKey(g))
		}
		if !reflect.DeepEqual(g.Object, w.Object) {
			t.Fatalf("%s object %s differs from the fixture's expected object", from, identityKey(g))
		}
		delete(byID, identityKey(g))
	}
}

// identityKey renders one object's declared identity for matching and
// failure reports.
func identityKey(obj *unstructured.Unstructured) string {
	return fmt.Sprintf("%s %s %s/%s",
		obj.GetAPIVersion(), obj.GetKind(), obj.GetNamespace(), obj.GetName())
}

// assertDeclaredObjects checks every object a driver declares carries
// the identity bootstrap polls by (apiVersion, kind, name) and is
// recognized by the driver's own Reconciled, which covers the declared
// objects — sync wiring and engine bundle.
func assertDeclaredObjects(t *testing.T, p Provider, objs []*unstructured.Unstructured, from string) {
	t.Helper()
	for i, obj := range objs {
		if obj == nil {
			t.Fatalf("%s object %d is nil", from, i)
		}
		if obj.GetAPIVersion() == "" || obj.GetKind() == "" || obj.GetName() == "" {
			t.Fatalf("%s object %d lacks a declared identity: apiVersion=%q kind=%q name=%q",
				from, i, obj.GetAPIVersion(), obj.GetKind(), obj.GetName())
		}
		if _, _, err := p.Reconciled(obj); err != nil {
			t.Fatalf("%s object %d (%s %s) not recognized by Reconciled: %v",
				from, i, obj.GetKind(), obj.GetName(), err)
		}
	}
}

// assertEngineBundle checks the documented EngineObjects consistency for
// one spec: an empty bundle (the degenerate case — the substrate doubles
// as the engine) or declared objects among which EngineNamespace names a
// Namespace object.
func assertEngineBundle(t *testing.T, p Provider, spec v1alpha1.EngineSpec, name string) {
	t.Helper()
	objs, err := p.EngineObjects(t.Context(), spec)
	if err != nil {
		t.Fatalf("EngineObjects(%s): %v", name, err)
	}
	if len(objs) == 0 {
		return
	}
	assertDeclaredObjects(t, p, objs, "EngineObjects("+name+")")
	ns := p.EngineNamespace()
	for _, obj := range objs {
		if obj.GetKind() == "Namespace" && obj.GetName() == ns {
			return
		}
	}
	t.Fatalf("EngineObjects(%s): non-empty bundle has no Namespace object named %q (EngineNamespace)",
		name, ns)
}

// assertStatusJudgments checks Reconciled over the driver's status
// fixtures. The no-stale-success principle is enforced here: a
// fresh-looking success with an outdated freshness marker must not
// count as reconciled, in whatever vocabulary the driver's CRs carry.
func assertStatusJudgments(t *testing.T, p Provider, fx StatusFixtures) {
	t.Helper()
	cases := []struct {
		name string
		obj  *unstructured.Unstructured
		want bool
	}{
		{"Ready", fx.Ready, true},
		{"NotReady", fx.NotReady, false},
		{"Stale", fx.Stale, false},
		{"UnknownStatus", fx.UnknownStatus, false},
	}
	for _, c := range cases {
		if c.obj == nil {
			t.Fatalf("status fixture %s is required", c.name)
		}
		got, reason, err := p.Reconciled(c.obj)
		if err != nil {
			t.Fatalf("Reconciled(%s): %v", c.name, err)
		}
		if got != c.want {
			t.Fatalf("Reconciled(%s) = %v, want %v", c.name, got, c.want)
		}
		if !c.want && reason == "" {
			t.Fatalf("Reconciled(%s) gave no reason; not-reconciled judgments must carry one", c.name)
		}
	}
	assertUnrecognized(t, p, fx.Unrecognized)
}

// assertUnrecognized checks an object outside the driver's coverage
// yields the domain's coded unrecognized-object error.
func assertUnrecognized(t *testing.T, p Provider, obj *unstructured.Unstructured) {
	t.Helper()
	if obj == nil {
		t.Fatal("status fixture Unrecognized is required")
	}
	_, _, err := p.Reconciled(obj)
	var coded *cubeerr.Coded
	if !errors.As(err, &coded) {
		t.Fatalf("Reconciled(Unrecognized) = %v, want *cubeerr.Coded", err)
	}
	if coded.Code != CodeUnrecognizedObject {
		t.Fatalf("code = %s, want %s", coded.Code, CodeUnrecognizedObject)
	}
}

// assertSpecValidation exercises the optional SpecValidator capability
// against driver-owned fixtures: every valid spec passes, every invalid
// spec yields its declared coded identity. The sub-test runs iff the
// capability is implemented, and never vacuously: a capability without
// fixtures — or fixtures without the capability — fails the suite.
func assertSpecValidation(t *testing.T, p Provider, fx Fixtures) {
	t.Helper()
	v, ok := p.(SpecValidator)
	if !ok {
		if len(fx.ValidSpecs)+len(fx.InvalidSpecs) > 0 {
			t.Fatal("spec fixtures supplied but the driver does not implement SpecValidator")
		}
		return
	}
	if len(fx.ValidSpecs) == 0 || len(fx.InvalidSpecs) == 0 {
		t.Fatal("SpecValidator drivers must supply at least one valid and one invalid spec fixture")
	}
	for i, s := range fx.ValidSpecs {
		if err := v.ValidateSpec(s); err != nil {
			t.Fatalf("ValidateSpec(valid %d): %v", i, err)
		}
	}
	for _, s := range fx.InvalidSpecs {
		err := v.ValidateSpec(s.Spec)
		var coded *cubeerr.Coded
		if !errors.As(err, &coded) {
			t.Fatalf("ValidateSpec(%s) = %v, want *cubeerr.Coded", s.Name, err)
		}
		if coded.Code != s.Code {
			t.Fatalf("ValidateSpec(%s) code = %s, want %s", s.Name, coded.Code, s.Code)
		}
	}
}
