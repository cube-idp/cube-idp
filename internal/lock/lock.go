// Package lock reads and writes cube.lock: the reproducibility record of an
// `up` — resolved pack pins, rendered-content hashes, and the full image
// list (spec §4.1 pack engine; feeds Phase 3 `vendor` and `upgrade --plan`).
package lock

import (
	"fmt"
	"os"
	"path/filepath"

	"sigs.k8s.io/yaml"

	"github.com/cube-idp/cube-idp/internal/diag"
)

// File is the top-level shape of cube.lock.
type File struct {
	APIVersion string     `yaml:"apiVersion" json:"apiVersion"`
	Kind       string     `yaml:"kind" json:"kind"`
	Engine     EngineLock `yaml:"engine" json:"engine"`
	Packs      []Entry    `yaml:"packs" json:"packs"`
}

// EngineLock records the GitOps engine: its type plus (engine-as-pack,
// 2026-07-19) the same reproducibility fields every pack Entry carries —
// the engine pack is pinnable and vendorable. All pack fields omitempty:
// locks written before engine-as-pack carried only type.
type EngineLock struct {
	Type         string   `yaml:"type" json:"type"`
	Ref          string   `yaml:"ref,omitempty" json:"ref,omitempty"`
	Name         string   `yaml:"name,omitempty" json:"name,omitempty"`
	Version      string   `yaml:"version,omitempty" json:"version,omitempty"`
	Resolved     string   `yaml:"resolved,omitempty" json:"resolved,omitempty"`
	RenderedHash string   `yaml:"renderedHash,omitempty" json:"renderedHash,omitempty"`
	Images       []string `yaml:"images,omitempty" json:"images,omitempty"`
}

// Entry projects the engine's pack fields as a lock.Entry so bundle
// vendoring and ref resolution treat the engine pack like every pack.
func (e EngineLock) Entry() Entry {
	return Entry{Ref: e.Ref, Name: e.Name, Version: e.Version,
		Resolved: e.Resolved, RenderedHash: e.RenderedHash, Images: e.Images}
}

// Entry is the reproducibility record for one delivered pack.
type Entry struct {
	Ref          string `yaml:"ref" json:"ref"`
	Name         string `yaml:"name" json:"name"`
	Version      string `yaml:"version" json:"version"`
	Resolved     string `yaml:"resolved" json:"resolved"`
	RenderedHash string `yaml:"renderedHash" json:"renderedHash"`
	// ValuesRef/ValuesPin record the pack's remote values source and its
	// resolved pin (spec 2026-07-19 §6) — absent for inline-only packs, so
	// ref-less locks stay byte-identical to pre-RV2 output (omitempty).
	ValuesRef string `yaml:"valuesRef,omitempty" json:"valuesRef,omitempty"`
	ValuesPin string `yaml:"valuesPin,omitempty" json:"valuesPin,omitempty"`
	// Images is the sorted union of every container image this pack pulls:
	// images found by walking the rendered manifests (lock.ImagesFrom) PLUS
	// any images the pack declares itself via pack.cue's optional images:
	// list (spec D14) — operator-style packs (e.g. envoy-gateway) provision
	// images that never appear in their own rendered objects, so the
	// declared list closes that air-gap blind spot. `up`'s lock assembly
	// computes the merge; `cube-idp vendor` (Phase 3) consumes it unchanged
	// to bundle every pinned image for air-gapped installs.
	Images []string `yaml:"images" json:"images"`
}

// PathFor returns the cube.lock path that sits next to cfgPath (cube.yaml).
func PathFor(cfgPath string) string {
	return filepath.Join(filepath.Dir(cfgPath), "cube.lock")
}

// Write serializes f to path deterministically (sigs.k8s.io/yaml marshals
// via JSON with sorted map keys).
func Write(path string, f *File) error {
	out, err := yaml.Marshal(f)
	if err != nil {
		return diag.Wrap(err, diag.CodeLockCorrupt, "cannot serialize cube.lock", "this is a cube-idp bug — please report it")
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		return diag.Wrap(err, diag.CodeLockCorrupt, fmt.Sprintf("cannot write %s", path), "check directory permissions")
	}
	return nil
}

// Read loads cube.lock from path. A missing file is not an error — it
// returns (nil, nil) so callers can distinguish "no lock yet" from "corrupt
// lock". A present-but-unparseable file is CUBE-0003.
func Read(path string) (*File, error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, diag.Wrap(err, diag.CodeLockCorrupt, fmt.Sprintf("cannot read %s", path), "check file permissions")
	}
	var f File
	if err := yaml.Unmarshal(raw, &f); err != nil {
		return nil, diag.Wrap(err, diag.CodeLockCorrupt, fmt.Sprintf("%s is corrupt", path),
			"delete it and re-run `cube-idp up` to regenerate")
	}
	return &f, nil
}
