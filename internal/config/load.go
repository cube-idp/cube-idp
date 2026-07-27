// Package config loads and validates the cube-idp Config document.
// Pipeline order is fixed: strict decode → Default → Validate.
package config

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"sigs.k8s.io/yaml"

	v1alpha1 "github.com/cube-idp/cube-idp/api/config/v1alpha1"
)

// Load reads, strictly decodes, defaults, and validates a Config from fsys.
// Fail fast: a non-nil *Config is always complete and valid.
func Load(fsys fs.FS, path string) (*v1alpha1.Config, error) {
	raw, err := fs.ReadFile(fsys, path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	return decode(raw)
}

// LoadFile is an os-filesystem convenience wrapper around Load.
func LoadFile(path string) (*v1alpha1.Config, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve config path %s: %w", path, err)
	}
	return Load(os.DirFS(filepath.Dir(abs)), filepath.Base(abs))
}

func decode(raw []byte) (*v1alpha1.Config, error) {
	var tm struct {
		APIVersion string `json:"apiVersion"`
		Kind       string `json:"kind"`
	}
	if err := yaml.Unmarshal(raw, &tm); err != nil {
		return nil, fmt.Errorf("determine apiVersion/kind: %w", err)
	}

	gvk := tm.APIVersion + "/" + tm.Kind
	switch gvk {
	case v1alpha1.GroupVersion.String() + "/Config":
		var c v1alpha1.Config
		if err := yaml.UnmarshalStrict(raw, &c); err != nil {
			return nil, errUnknownField(err)
		}
		c.Default()
		if errs := c.Validate(); len(errs) > 0 {
			return nil, errInvalidConfig(errs.ToAggregate())
		}
		return &c, nil
	default:
		return nil, errUnsupportedAPIVersion(gvk)
	}
}
