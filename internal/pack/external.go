package pack

import (
	"context"
	"encoding/json"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	v1alpha1 "github.com/cube-idp/cube-idp/api/config/v1alpha1"
)

// externalGroups are one instance's external manifests, grouped the way
// RenderPlan carries them: prerequisites apart from the objects delivered
// alongside the pack.
type externalGroups struct {
	pre  []*unstructured.Unstructured
	with []*unstructured.Unstructured
}

// resolveExternalManifests turns every declared entry into exactly one object,
// grouped by lifecycle and in declaration order.
//
// A declared group can never come back empty, so CUBE-PKG-007 has no path
// here: an entry yields one object or a coded error, and an entry resolving to
// none is already CUBE-PKG-014.
func resolveExternalManifests(
	ctx context.Context,
	entries []v1alpha1.ExternalManifest,
	resolve resolveDocumentFunc,
) (externalGroups, error) {
	var groups externalGroups
	for i, entry := range entries {
		if err := ctx.Err(); err != nil {
			return externalGroups{}, err
		}
		obj, err := externalObject(ctx, i, entry, resolve)
		if err != nil {
			return externalGroups{}, err
		}
		// The lifecycle enum belongs to the document layer, which rejects any
		// other spelling; empty defaults to "with", so anything that is not
		// "pre" is delivered alongside the pack.
		if entry.Lifecycle == v1alpha1.LifecyclePre {
			groups.pre = append(groups.pre, obj)
			continue
		}
		groups.with = append(groups.with, obj)
	}
	return groups, nil
}

// externalObject resolves one entry to the single object it carries.
//
// Ref and Manifest are exclusive, and an entry carrying neither never reaches
// here from a validated document; one that does is answered by the resolver's
// own malformed-reference error rather than by a guess made here.
func externalObject(
	ctx context.Context,
	i int,
	entry v1alpha1.ExternalManifest,
	resolve resolveDocumentFunc,
) (*unstructured.Unstructured, error) {
	if entry.Manifest != nil {
		return inlineManifest(i, entry.Manifest)
	}
	data, err := resolve(ctx, entry.Ref)
	if err != nil {
		// The resolver's coded error already names the reference; the field it
		// came from is the context the caller lacks.
		return nil, fmt.Errorf("externalManifests[%d].ref: %w", i, err)
	}

	docs, err := decodeDocuments(data)
	if err != nil || len(docs) != 1 || !isKubernetesObject(docs[0]) {
		return nil, newExternalManifestError(entry.Ref, len(docs), err)
	}
	return &unstructured.Unstructured{Object: docs[0]}, nil
}

// inlineManifest decodes an inline entry, which api/ keeps as opaque JSON.
//
// That it is a Kubernetes object is the document layer's check (CUBE-CFG-*),
// and it is not repeated: an inline entry's provenance is the config document
// itself, so the schema-level feedback an author gets there is the point of
// writing it inline rather than behind a reference.
func inlineManifest(i int, raw *runtime.RawExtension) (*unstructured.Unstructured, error) {
	obj := map[string]any{}
	if err := json.Unmarshal(raw.Raw, &obj); err != nil {
		return nil, newManifestParseError(fmt.Sprintf("externalManifests[%d].manifest", i), err)
	}
	return &unstructured.Unstructured{Object: obj}, nil
}

// isKubernetesObject reports whether a decoded document is recognisable as one
// Kubernetes object. Whether it is valid for its kind is the API server's
// business; this is the part a pack author gets wrong in a file they point at.
func isKubernetesObject(doc map[string]any) bool {
	apiVersion, _ := doc["apiVersion"].(string)
	kind, _ := doc["kind"].(string)
	return apiVersion != "" && kind != ""
}
