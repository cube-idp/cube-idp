package gateway

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
)

// CRDsVersion is the pinned Gateway API release the embedded CRDs pack
// vendors, in the repo's clean SemVer spelling (upstream tags it v1.6.1).
// Regenerate the payload with `make gateway-api-manifests` and update
// crdsSHA256 to match; the provenance check enforces both.
const CRDsVersion = "1.6.1"

// ChartVersion is the pinned Traefik chart version the thin-helm pack
// delegates to.
const ChartVersion = "41.3.0"

// ChartDigest pins the Traefik chart OCI artifact bit-for-bit; the tag
// stays beside it for legibility and the digest is what actually pins.
const ChartDigest = "sha256:dcae2d586d7fbda6a08150eaeeca4132e9dd042d8a4d16ada287e8c40f6ff17a"

// crdsSHA256 pins the vendored Gateway API payload's content. A mismatch
// means the embedded asset drifted from its recorded provenance.
const crdsSHA256 = "24d931f22abd8e40c973264319ead7cfa09d0fb7716b7ab1ee2ff174cb063a73"

// The embedded pack directories, under packs/ so each pack.cue sits at
// the root of its own fs.FS. The Go source beside them is not embedded,
// so each sub-FS is exactly the pack.
const (
	crdsPackDir = "packs/gateway-api-crds"
	helmPackDir = "packs/traefik-gateway"
)

// crdsPayloadPath is the payload file inside the embedded CRDs pack
// directory, relative to that pack's root.
const crdsPayloadPath = "manifests/standard-install.yaml"

// packsFS embeds both prerequisite pack directories.
//
//go:embed packs
var packsFS embed.FS

// CRDsPackFS returns the embedded Gateway API CRDs pack directory, rooted
// so pack.cue is at the top — ready for pack.Load at the composition edge
// (this package itself never imports internal/pack).
func CRDsPackFS() (fs.FS, error) { return fs.Sub(packsFS, crdsPackDir) }

// HelmPackFS returns the embedded traefik gateway pack directory, rooted
// so pack.cue is at the top — ready for pack.Load at the composition
// edge.
func HelmPackFS() (fs.FS, error) { return fs.Sub(packsFS, helmPackDir) }

// crdsPayload returns a copy of the embedded Gateway API payload after
// verifying it against the recorded sha256 provenance.
func crdsPayload() ([]byte, error) {
	data, err := fs.ReadFile(packsFS, crdsPackDir+"/"+crdsPayloadPath)
	if err != nil {
		return nil, newPackProvenanceError("payload unreadable", err)
	}
	sum := sha256.Sum256(data)
	if got := hex.EncodeToString(sum[:]); got != crdsSHA256 {
		return nil, newPackProvenanceError(
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
			return nil, newPackParseError(err)
		}
		if len(m) == 0 {
			continue
		}
		objs = append(objs, &unstructured.Unstructured{Object: m})
	}
	return objs, nil
}
