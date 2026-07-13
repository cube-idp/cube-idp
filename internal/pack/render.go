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

	// Render precedence: if kustomization.yaml exists at the pack root, it
	// is the sole source of raw manifests and governs manifests/ entirely
	// (as `resources:`) — manifests/ is never walked independently, to
	// avoid double-rendering the same objects. Otherwise the Phase 1
	// behavior (walk manifests/*.yaml directly) is unchanged. chart.yaml
	// helm rendering below is orthogonal and appended in both cases.
	manifestsDir := filepath.Join(p.Dir, "manifests")
	if _, statErr := os.Stat(filepath.Join(p.Dir, "kustomization.yaml")); statErr == nil {
		objs, err := RenderDir(p.Dir)
		if err != nil {
			return nil, err
		}
		r.Objects = append(r.Objects, objs...)
	} else {
		// A missing manifests/ dir or chart.yaml simply means that optional
		// part of the pack is absent; any OTHER error (e.g. permissions) is
		// real and must surface rather than silently rendering a partial pack.
		entries, err := os.ReadDir(manifestsDir)
		if err != nil && !os.IsNotExist(err) {
			return nil, diag.Wrap(err, diag.CodePackManifestErr, "cannot read pack manifests/ directory", "check directory permissions")
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
		for _, e := range entries {
			if e.IsDir() || (filepath.Ext(e.Name()) != ".yaml" && filepath.Ext(e.Name()) != ".yml") {
				continue
			}
			raw, err := os.ReadFile(filepath.Join(manifestsDir, e.Name()))
			if err != nil {
				return nil, diag.Wrap(err, diag.CodePackManifestErr, "cannot read pack manifest "+e.Name(), "check file permissions")
			}
			objs, err := apply.ParseMultiDoc(raw)
			if err != nil {
				return nil, diag.Wrap(err, diag.CodePackManifestErr, p.Name+"/"+e.Name()+" is not valid YAML", "fix the manifest")
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
	} else if !os.IsNotExist(err) {
		return nil, diag.Wrap(err, diag.CodePackManifestErr, "cannot read pack chart.yaml", "check file permissions")
	}

	if len(r.Objects) == 0 {
		return nil, diag.New(diag.CodePackManifestErr, "pack "+p.Name+" rendered zero objects",
			"a pack needs manifests/ and/or chart.yaml")
	}
	return r, nil
}
