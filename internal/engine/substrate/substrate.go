// Package substrate owns the invariant tier-1 Flux substrate: the
// embedded, pinned install pack (pack.cue plus the vendored manifests
// beside this file), the substrate-namespace fact, and the version pin
// with its provenance check. It is platform, not a driver — never
// selected by spec.engine.provider, and nothing about it crosses the
// engine seam. The substrate parses its own payload with raw-pack
// semantics and does not import internal/pack; a dogfood test at the
// composition edge enforces the equivalence. Contract:
// docs/domains/engine.md.
package substrate

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"

	"github.com/cube-idp/cube-idp/internal/engine"
)

// Version is the pinned Flux release the embedded substrate pack
// carries, in the repo's clean SemVer spelling. Upstream spells it
// v2.9.2; the substrate alone maps between the spellings where the
// vendored asset or upstream tags require it. Regenerate the payload
// with `make flux-manifests` and update manifestsSHA256 to match; the
// provenance check enforces both.
const Version = "2.9.2"

// Namespace is the invariant substrate namespace: where the substrate
// lives and where bootstrap records its inventory. The edge injects it
// into bootstrap as a string; a test ties the fact to a Namespace
// object present in the embedded payload.
const Namespace = "flux-system"

// manifestsPath is the payload file inside the embedded pack directory.
const manifestsPath = "manifests/flux.yaml"

// manifestsSHA256 pins the vendored payload's content. A mismatch means
// the embedded manifests drifted from their recorded provenance.
const manifestsSHA256 = "f229fa8ace1655b04ccb2566fa3169681061b48c8e1b7a4e72b7d2413915bb84"

// packFS embeds the substrate pack directory: pack.cue at the root plus
// the manifests payload. The Go source beside them is not embedded, so
// the FS is exactly the pack.
//
//go:embed pack.cue manifests
var packFS embed.FS

// FS returns the embedded substrate pack directory, rooted so pack.cue
// is at the top — ready for pack.Load at the composition edge (this
// package itself never imports internal/pack).
func FS() fs.FS { return packFS }

// Objects returns the substrate's install objects: the provenance-
// verified payload parsed with raw-pack semantics — multi-document YAML
// in document order, empty documents skipped, an empty result rejected.
// The composition-edge dogfood test pins the equivalence with rendering
// the same embedded directory through internal/pack.
func Objects() ([]*unstructured.Unstructured, error) {
	data, err := manifests()
	if err != nil {
		return nil, err
	}
	objs, err := parseManifests(data)
	if err != nil {
		return nil, err
	}
	if len(objs) == 0 {
		return nil, engine.NewSubstrateParseError(errors.New("payload holds no objects"))
	}
	return objs, nil
}

// CheckVersion asserts a requested spec.engine.version against the
// pinned substrate: empty selects the pin, and only the clean SemVer
// spelling matches — "v2.9.2" is a mismatch, per the repo's versioning
// rule (upstream's v-prefixed spelling never crosses the config
// surface).
func CheckVersion(requested string) error {
	if requested != "" && requested != Version {
		return engine.NewVersionMismatchError(requested, Version)
	}
	return nil
}

// manifests returns a copy of the embedded payload after verifying it
// against the recorded sha256 provenance.
func manifests() ([]byte, error) {
	data, err := fs.ReadFile(packFS, manifestsPath)
	if err != nil {
		return nil, engine.NewSubstrateProvenanceError("payload unreadable", err)
	}
	sum := sha256.Sum256(data)
	if got := hex.EncodeToString(sum[:]); got != manifestsSHA256 {
		return nil, engine.NewSubstrateProvenanceError(
			fmt.Sprintf("sha256 %s does not match the pin", got), nil)
	}
	return data, nil
}

// parseManifests splits a multi-document YAML stream into unstructured
// objects, skipping empty documents.
func parseManifests(data []byte) ([]*unstructured.Unstructured, error) {
	dec := utilyaml.NewYAMLOrJSONDecoder(bytes.NewReader(data), 4096)
	var objs []*unstructured.Unstructured
	for {
		m := map[string]any{}
		if err := dec.Decode(&m); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, engine.NewSubstrateParseError(err)
		}
		if len(m) == 0 {
			continue
		}
		objs = append(objs, &unstructured.Unstructured{Object: m})
	}
	return objs, nil
}
