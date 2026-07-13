package apply

import (
	"bytes"
	"io"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	kyaml "k8s.io/apimachinery/pkg/util/yaml"

	"github.com/rafpe/cube-idp/internal/diag"
)

// ParseMultiDoc splits a multi-document YAML stream (e.g. the output of a
// Helm template render or a raw manifest bundle) into unstructured objects.
// Empty documents (blank docs between "---" separators, trailing
// whitespace-only docs) are skipped rather than erroring.
func ParseMultiDoc(data []byte) ([]*unstructured.Unstructured, error) {
	decoder := kyaml.NewYAMLOrJSONDecoder(bytes.NewReader(data), 4096)
	var objs []*unstructured.Unstructured
	for {
		var raw map[string]any
		if err := decoder.Decode(&raw); err != nil {
			if err == io.EOF {
				break
			}
			return nil, diag.Wrap(err, diag.CodeApplyParseYAML, "cannot parse manifest YAML", "check the manifest for syntax errors")
		}
		if len(raw) == 0 {
			continue // blank document between "---" separators
		}
		objs = append(objs, &unstructured.Unstructured{Object: raw})
	}
	return objs, nil
}
