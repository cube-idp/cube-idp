package pack

import (
	"os"
	"path/filepath"
	"sort"

	"github.com/rafpe/cube-idp/internal/apply"
	"github.com/rafpe/cube-idp/internal/config"
	"github.com/rafpe/cube-idp/internal/diag"
)

// Render validates values against the pack's #Values schema (if any), then
// produces the pack's final manifests: raw YAML under manifests/ (in sorted
// filename order, for deterministic output) plus a client-side helm render
// of chart.yaml, if present. It is RenderFor with a zero
// config.GatewaySpec{}, which performs no ${GATEWAY_HOST}/${GATEWAY_FQDN}
// substitution — existing callers/tests see byte-identical output to
// before D15.
func (p *Pack) Render(values map[string]any) (*Rendered, error) {
	return p.RenderFor(values, config.GatewaySpec{})
}

// RenderFor is Render plus the D15 gateway substitution (spec D15, Owner
// Decisions #11): every string leaf in the chart values (pack defaults from
// chart.yaml merged with the caller's values, override winning) and every
// manifests/*.yaml file's raw bytes get ${GATEWAY_HOST} -> gw's host[:port],
// ${GATEWAY_FQDN} -> gw's bare host, and ${GATEWAY_PACK} -> gw's pack name
// (also its namespace, for F9 HTTPRoute parentRefs) (see substitute in
// expose.go — the replacements ExposeURLs already applies to expose.urls). A zero
// gw (Host == "") performs no substitution, so Render(values) — which calls
// this with config.GatewaySpec{} — is unaffected.
func (p *Pack) RenderFor(values map[string]any, gw config.GatewaySpec) (*Rendered, error) {
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
	_, statErr := os.Stat(filepath.Join(p.Dir, "kustomization.yaml"))
	switch {
	case statErr == nil:
		objs, err := RenderDir(p.Dir)
		if err != nil {
			return nil, err
		}
		r.Objects = append(r.Objects, objs...)
	case !os.IsNotExist(statErr):
		// A missing kustomization.yaml simply means the pack doesn't use
		// one; any OTHER stat error (e.g. permissions, a symlink loop) is
		// real and must surface rather than silently falling through to the
		// manifests/ walk below as if kustomization.yaml were just absent.
		return nil, diag.Wrap(statErr, diag.CodePackManifestErr, "cannot check pack kustomization.yaml", "check directory permissions")
	default:
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
			// D15: substitute before parsing, on the raw bytes — a plain
			// text replacement over the manifest source, same as
			// ExposeURLs does for expose.urls, rather than a structural
			// walk over the parsed objects.
			objs, err := apply.ParseMultiDoc([]byte(substitute(string(raw), gw)))
			if err != nil {
				return nil, diag.Wrap(err, diag.CodePackManifestErr, p.Name+"/"+e.Name()+" is not valid YAML", "fix the manifest")
			}
			r.Objects = append(r.Objects, objs...)
		}
	}

	if _, err := os.Stat(filepath.Join(p.Dir, "chart.yaml")); err == nil {
		objs, err := renderHelm(p.Dir, vals, gw)
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
