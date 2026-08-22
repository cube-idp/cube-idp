package pack

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"maps"
	"path"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
)

// helmMilestone names where the one still-unimplemented render type lands, so
// the not-implemented error points somewhere real.
const helmMilestone = "a milestone of its own, with the helm dependency"

// RenderOptions carries the render inputs that come from the setup rather
// than from the pack itself.
type RenderOptions struct {
	// Values are validated against the pack's #Values definition. They are
	// meaningful only to helm and kustomize packs; a raw pack rejects them.
	Values map[string]any
}

// RenderPlan is the result of rendering one pack: the objects the pack itself
// produces, plus the prerequisite objects declared beside it.
//
// The two groups are separate because they become separate delivery units:
// prerequisites need their own readiness gate before the pack's own objects
// are reconciled. This build populates Objects only — external manifests, and
// therefore prerequisites, arrive with the setup surface that declares them.
type RenderPlan struct {
	// Prerequisites are delivered, and must become ready, before Objects.
	Prerequisites []*unstructured.Unstructured
	// Objects are the pack's own rendered objects.
	Objects []*unstructured.Unstructured
}

// Render turns the pack into a RenderPlan. It is deterministic and
// cluster-independent: the same pack and options always produce the same
// objects in the same order, with no timestamps, generated identifiers, or
// environment-derived fields.
func (p *Pack) Render(ctx context.Context, opts RenderOptions) (RenderPlan, error) {
	if err := ctx.Err(); err != nil {
		return RenderPlan{}, err
	}
	// Values are resolved before the type dispatch so that bad values are
	// reported for every pack type, not only the ones this build can render.
	values, err := p.resolveValues(maps.Clone(opts.Values))
	if err != nil {
		return RenderPlan{}, err
	}

	switch p.meta.Type {
	case TypeRaw:
		return p.planRaw(ctx)
	case TypeKustomize:
		return p.planKustomize(ctx, values)
	default:
		return RenderPlan{}, newRenderTypeUnsupportedError(p.meta.Type, helmMilestone)
	}
}

// planRaw renders a raw pack: its manifests, in the namespace the pack forces.
// Raw packs have no templating step, so no substitution applies — values are
// already rejected for them at resolveValues.
func (p *Pack) planRaw(ctx context.Context) (RenderPlan, error) {
	objs, err := p.renderRaw(ctx)
	if err != nil {
		return RenderPlan{}, err
	}
	if err := applyNamespace(objs, p.meta.Namespace); err != nil {
		return RenderPlan{}, err
	}
	return RenderPlan{Objects: objs}, nil
}

// planKustomize builds the kustomization, then applies the pack's own
// semantics on top of the result: the same namespace transform raw packs get,
// followed by ${VAR} substitution.
//
// The values are narrowed to flat strings first, so a values mistake is
// reported before the build runs rather than after it.
func (p *Pack) planKustomize(ctx context.Context, values map[string]any) (RenderPlan, error) {
	vars, err := flatStringValues(values)
	if err != nil {
		return RenderPlan{}, err
	}
	objs, err := p.renderKustomize(ctx)
	if err != nil {
		return RenderPlan{}, err
	}
	if err := applyNamespace(objs, p.meta.Namespace); err != nil {
		return RenderPlan{}, err
	}
	if err := substitute(objs, vars); err != nil {
		return RenderPlan{}, err
	}
	return RenderPlan{Objects: objs}, nil
}

// renderRaw walks the pack's manifests directory and parses every manifest
// file into objects. fs.WalkDir visits entries in lexical order, so the object
// order is a function of the file names alone — stable across machines and
// runs.
func (p *Pack) renderRaw(ctx context.Context) ([]*unstructured.Unstructured, error) {
	var objs []*unstructured.Unstructured

	err := fs.WalkDir(p.fsys, ManifestsDir, func(name string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return newFileUnreadableError(name, err)
		case d.IsDir(), !isManifest(name):
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		data, err := fs.ReadFile(p.fsys, name)
		if err != nil {
			return newFileUnreadableError(name, err)
		}
		parsed, err := parseManifest(name, data)
		if err != nil {
			return err
		}
		objs = append(objs, parsed...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(objs) == 0 {
		return nil, newEmptyRenderError(p.meta.Name)
	}
	return objs, nil
}

// isManifest reports whether a file is a manifest by extension. The extension
// is the whole rule: a pack's manifests directory may hold README files or
// other non-manifest content without that becoming a parse error.
func isManifest(name string) bool {
	switch path.Ext(name) {
	case ".yaml", ".yml":
		return true
	default:
		return false
	}
}

// parseManifest splits one multi-document YAML file into objects, skipping
// empty documents (a trailing separator is not an error).
func parseManifest(name string, data []byte) ([]*unstructured.Unstructured, error) {
	dec := utilyaml.NewYAMLOrJSONDecoder(bytes.NewReader(data), 4096)
	var objs []*unstructured.Unstructured
	for {
		obj := map[string]any{}
		if err := dec.Decode(&obj); err != nil {
			if errors.Is(err, io.EOF) {
				return objs, nil
			}
			return nil, newManifestParseError(name, err)
		}
		if len(obj) == 0 {
			continue
		}
		objs = append(objs, &unstructured.Unstructured{Object: obj})
	}
}
