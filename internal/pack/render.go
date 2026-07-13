package pack

import (
	"os"
	"path/filepath"
	"sort"

	"github.com/rafpe/cube-idp/internal/apply"
	"github.com/rafpe/cube-idp/internal/diag"
)

// Render validates values against the pack's #Values schema (if any), then
// produces the pack's final manifests: raw YAML under manifests/ (in sorted
// filename order, for deterministic output) plus a client-side helm render
// of chart.yaml, if present.
func (p *Pack) Render(values map[string]any) (*Rendered, error) {
	vals, err := p.validateValues(values)
	if err != nil {
		return nil, err
	}
	r := &Rendered{Name: p.Name, Version: p.Version}

	manifestsDir := filepath.Join(p.Dir, "manifests")
	if entries, err := os.ReadDir(manifestsDir); err == nil {
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
		for _, e := range entries {
			if e.IsDir() || (filepath.Ext(e.Name()) != ".yaml" && filepath.Ext(e.Name()) != ".yml") {
				continue
			}
			raw, err := os.ReadFile(filepath.Join(manifestsDir, e.Name()))
			if err != nil {
				return nil, diag.Wrap(err, "CUBE-4004", "cannot read pack manifest "+e.Name(), "check file permissions")
			}
			objs, err := apply.ParseMultiDoc(raw)
			if err != nil {
				return nil, diag.Wrap(err, "CUBE-4004", p.Name+"/"+e.Name()+" is not valid YAML", "fix the manifest")
			}
			r.Objects = append(r.Objects, objs...)
		}
	}

	if _, err := os.Stat(filepath.Join(p.Dir, "chart.yaml")); err == nil {
		objs, err := renderHelm(p.Dir, vals)
		if err != nil {
			return nil, err
		}
		r.Objects = append(r.Objects, objs...)
	}

	if len(r.Objects) == 0 {
		return nil, diag.New("CUBE-4004", "pack "+p.Name+" rendered zero objects",
			"a pack needs manifests/ and/or chart.yaml")
	}
	return r, nil
}
