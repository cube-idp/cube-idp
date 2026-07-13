// Package lock reads and writes cube.lock: the reproducibility record of an
// `up` — resolved pack pins, rendered-content hashes, and the full image
// list (spec §4.1 pack engine; feeds Phase 3 `vendor` and `upgrade --plan`).
package lock

import (
	"fmt"
	"os"
	"path/filepath"

	"sigs.k8s.io/yaml"

	"github.com/rafpe/cube-idp/internal/diag"
)

// File is the top-level shape of cube.lock.
type File struct {
	APIVersion string     `yaml:"apiVersion" json:"apiVersion"`
	Kind       string     `yaml:"kind" json:"kind"`
	Engine     EngineLock `yaml:"engine" json:"engine"`
	Packs      []Entry    `yaml:"packs" json:"packs"`
}

// EngineLock records which GitOps reconciliation engine this cube uses.
type EngineLock struct {
	Type string `yaml:"type" json:"type"`
}

// Entry is the reproducibility record for one delivered pack.
type Entry struct {
	Ref          string   `yaml:"ref" json:"ref"`
	Name         string   `yaml:"name" json:"name"`
	Version      string   `yaml:"version" json:"version"`
	Resolved     string   `yaml:"resolved" json:"resolved"`
	RenderedHash string   `yaml:"renderedHash" json:"renderedHash"`
	Images       []string `yaml:"images" json:"images"`
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
