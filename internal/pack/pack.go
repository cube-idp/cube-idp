// Package pack implements cube-idp's extensibility tier 1 (spec §4.4):
// data-only directories with pack.cue metadata, fetched from local dirs or
// OCI, values-validated with CUE, rendered in-process.
//
// Pack format: a directory containing:
//
//	pack.cue          required: name, version; optional #Values schema
//	manifests/*.yaml  optional: raw multi-doc YAML manifests
//	chart.yaml        optional: a helm chart reference, rendered client-side
//	                   (spec §4: engines receive rendered manifests only;
//	                   helm-controller is not installed in-cluster)
//
// chart.yaml shape:
//
//	chart: traefik
//	repo: https://traefik.github.io/charts   # or oci://registry/chart
//	version: "34.1.0"
//	releaseName: traefik
//	namespace: traefik
//	values:                                  # chart-level defaults, merged
//	  ...                                     # UNDER user-supplied values
package pack

import (
	"fmt"
	"os"
	"path/filepath"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/rafpe/cube-idp/internal/diag"
)

// Pack is fetched + validated pack metadata: a local, on-disk directory
// whose pack.cue has already been parsed.
type Pack struct {
	Name    string
	Version string
	Dir     string
}

// Rendered is the final set of objects a pack produces for a given set of
// values: raw manifests plus (if the pack has one) a client-side helm
// template render. Task 9 pushes this as an OCI artifact; Task 10
// orchestrates Fetch -> Render -> push -> deliver.
type Rendered struct {
	Name    string
	Version string
	Objects []*unstructured.Unstructured
}

// loadMeta reads and validates pack.cue in dir, returning the pack's
// required name/version metadata.
func loadMeta(dir string) (*Pack, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "pack.cue"))
	if err != nil {
		return nil, diag.Wrap(err, "CUBE-4003", fmt.Sprintf("pack at %s has no pack.cue", dir),
			"every pack needs a pack.cue with at least name and version")
	}
	v := cuecontext.New().CompileBytes(raw)
	if v.Err() != nil {
		return nil, diag.Wrap(v.Err(), "CUBE-4003", "pack.cue does not compile", "fix the CUE syntax")
	}
	p := &Pack{Dir: dir}
	if err := v.LookupPath(cue.ParsePath("name")).Decode(&p.Name); err != nil || p.Name == "" {
		return nil, diag.New("CUBE-4003", "pack.cue is missing 'name'", "add: name: \"<pack-name>\"")
	}
	if err := v.LookupPath(cue.ParsePath("version")).Decode(&p.Version); err != nil || p.Version == "" {
		return nil, diag.New("CUBE-4003", "pack.cue is missing 'version'", "add: version: \"0.1.0\"")
	}
	return p, nil
}

// validateValues unifies user values with #Values (if declared in
// pack.cue) and returns the concrete, defaulted value map. Packs without a
// #Values schema accept any values map unchecked.
func (p *Pack) validateValues(values map[string]any) (map[string]any, error) {
	raw, err := os.ReadFile(filepath.Join(p.Dir, "pack.cue"))
	if err != nil {
		return nil, diag.Wrap(err, "CUBE-4003", fmt.Sprintf("pack at %s has no pack.cue", p.Dir),
			"every pack needs a pack.cue with at least name and version")
	}
	ctx := cuecontext.New()
	root := ctx.CompileBytes(raw)
	if root.Err() != nil {
		return nil, diag.Wrap(root.Err(), "CUBE-4003", "pack.cue does not compile", "fix the CUE syntax")
	}
	schema := root.LookupPath(cue.ParsePath("#Values"))
	if !schema.Exists() {
		return values, nil
	}
	unified := schema.Unify(ctx.Encode(values))
	if err := unified.Validate(cue.Concrete(true)); err != nil {
		return nil, diag.Wrap(err, "CUBE-4002",
			fmt.Sprintf("values for pack %q do not match its #Values schema", p.Name),
			"compare your values with the pack's pack.cue #Values definition")
	}
	var out map[string]any
	if err := unified.Decode(&out); err != nil {
		return nil, diag.Wrap(err, "CUBE-4002", "cannot decode validated values", "simplify the values to plain YAML types")
	}
	return out, nil
}
