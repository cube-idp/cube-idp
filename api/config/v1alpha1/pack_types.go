package v1alpha1

import (
	"k8s.io/apimachinery/pkg/runtime"
)

// PackSpec is one pack instance in the setup.
//
// Artifact identity — what the pack is — lives in the pack's own pack.cue as
// name plus version. This type carries the other identity: which installed
// copy this is, and how the setup configures it.
type PackSpec struct {
	// ID is this instance's identity within the setup (DNS-label). Empty
	// defaults to the pack's own name, and is required when that name occurs
	// more than once across spec.packs. Deriving the effective ID needs each
	// pack resolved, so that happens in internal/pack, not here.
	ID string `json:"id,omitempty"`

	// PackRef locates the pack artifact: a local path, or a remote reference
	// in the grammar internal/ref owns.
	PackRef string `json:"packRef"`

	// ValuesRef locates a base values document: exactly one YAML mapping.
	ValuesRef string `json:"valuesRef,omitempty"`

	// Values are merged over the ValuesRef document as an RFC 7386 merge
	// patch (null deletes, arrays replace). Opaque here: internal/pack merges
	// them and validates the result against the pack's #Values.
	Values *runtime.RawExtension `json:"values,omitempty"`

	// ExternalManifests are delivered beside the pack, grouped by lifecycle.
	ExternalManifests []ExternalManifest `json:"externalManifests,omitempty"`

	// DependsOn entries are the ID or the pack name of another instance in
	// the setup. Resolving them needs every pack loaded, so the graph is
	// built in internal/pack; this document only records the intent.
	DependsOn []string `json:"dependsOn,omitempty"`
}

// ExternalManifest is one Kubernetes object delivered beside a pack: either
// fetched from a reference or written inline, never both.
//
// One object per entry, rather than one opaque multi-document string, is what
// makes per-object lifecycle expressible at all — and it means a malformed
// object is a validation error here instead of a parse error much later.
type ExternalManifest struct {
	// Ref locates exactly one manifest document, in the grammar internal/ref
	// owns. Exactly one of Ref or Manifest is set.
	Ref string `json:"ref,omitempty"`

	// Manifest is one inline Kubernetes object, authored as nested YAML.
	// Exactly one of Ref or Manifest is set.
	Manifest *runtime.RawExtension `json:"manifest,omitempty"`

	// Lifecycle says when this manifest is delivered; empty defaults to
	// LifecycleWith.
	Lifecycle Lifecycle `json:"lifecycle,omitempty"`
}

// Lifecycle says when an external manifest is delivered relative to its pack.
type Lifecycle string

const (
	// LifecyclePre delivers the manifest before the pack, as a prerequisite
	// that must become ready first.
	LifecyclePre Lifecycle = "pre"
	// LifecycleWith delivers the manifest alongside the pack's own objects.
	LifecycleWith Lifecycle = "with"
)
