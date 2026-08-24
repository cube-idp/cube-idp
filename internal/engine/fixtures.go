package engine

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	v1alpha1 "github.com/cube-idp/cube-idp/api/config/v1alpha1"
	"github.com/cube-idp/cube-idp/internal/cubeerr"
)

// Fixtures are the driver-supplied inputs RunEngineConformance exercises
// a Provider against. Every payload is owned by the driver under test —
// the suite hardcodes none, so nothing in it silently assumes one
// driver's vocabulary.
type Fixtures struct {
	// NoSource is a valid engine spec without a source; the suite
	// asserts SourceObjects yields no objects and no error for it.
	NoSource v1alpha1.EngineSpec

	// Sources are valid engine specs with a source configured; the
	// suite asserts SourceObjects yields exactly each fixture's
	// expected objects. At least one is required.
	Sources []SourceFixture

	// Statuses are the status-judgment fixtures for Reconciled. All
	// are required.
	Statuses StatusFixtures

	// ValidSpecs and InvalidSpecs feed the optional SpecValidator
	// sub-test: required (at least one each) iff the driver implements
	// the capability, and must be absent otherwise.
	ValidSpecs   []v1alpha1.EngineSpec
	InvalidSpecs []InvalidSpecFixture
}

// SourceFixture is one configured-source engine spec plus the driver's
// own statement of the sync wiring it must produce for it.
type SourceFixture struct {
	// Name labels the fixture in failure reports.
	Name string
	// Spec is the engine spec with a configured source.
	Spec v1alpha1.EngineSpec
	// Want are the objects SourceObjects must return for Spec —
	// exactly these, in any order, compared deeply. They are the
	// fixture's hand-authored statement of the git|oci contract in the
	// driver's vocabulary, pinning source-derived content (url, ref,
	// path, interval), not just identities; at least one is required.
	Want []*unstructured.Unstructured
}

// StatusFixtures are driver-owned objects covering every documented
// Reconciled judgment, expressed in the driver's own CRs' status
// vocabulary.
type StatusFixtures struct {
	// Ready must judge reconciled.
	Ready *unstructured.Unstructured
	// NotReady must judge not reconciled, with a reason.
	NotReady *unstructured.Unstructured
	// Stale carries a fresh-looking success whose freshness marker is
	// outdated (for flux: Ready true with status.observedGeneration
	// behind metadata.generation); it must judge not reconciled — no
	// stale success may count.
	Stale *unstructured.Unstructured
	// UnknownStatus is a recognized object whose status is absent or
	// unknown; it must judge not reconciled.
	UnknownStatus *unstructured.Unstructured
	// Unrecognized is an object outside the driver's declared
	// coverage; it must yield the coded unrecognized-object error.
	Unrecognized *unstructured.Unstructured
}

// InvalidSpecFixture is one engine spec the driver's SpecValidator must
// reject, with the coded identity the rejection must carry.
type InvalidSpecFixture struct {
	Name string
	Spec v1alpha1.EngineSpec
	Code cubeerr.Code
}
