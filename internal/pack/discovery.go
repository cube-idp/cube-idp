package pack

import (
	_ "embed"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/cube-idp/cube-idp/internal/apply"
)

//go:embed manifests/pack-crd.yaml
var packCRDYAML []byte

// CRD returns the inert packs.cube-idp.dev CRD (D11): applied by `up`,
// inventory-tracked, deleted by `down`, reconciled by NOBODY.
func CRD() (*unstructured.Unstructured, error) {
	objs, err := apply.ParseMultiDoc(packCRDYAML)
	if err != nil {
		return nil, err
	}
	return objs[0], nil
}
