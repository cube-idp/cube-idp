package gateway

import (
	"errors"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// CRDsPackObjects returns the embedded Gateway API CRDs, parsed with
// raw-pack semantics from the provenance-verified payload: multi-document
// YAML in document order, empty documents skipped, an empty result
// rejected. The composition-edge dogfood test pins the equivalence with
// rendering the same embedded directory through internal/pack.
func CRDsPackObjects() ([]*unstructured.Unstructured, error) {
	data, err := crdsPayload()
	if err != nil {
		return nil, err
	}
	objs, err := parseManifests(data)
	if err != nil {
		return nil, err
	}
	if len(objs) == 0 {
		return nil, newPackParseError(errors.New("payload holds no objects"))
	}
	return objs, nil
}
