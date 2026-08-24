package pack

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"path"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"

	v1alpha1 "github.com/cube-idp/cube-idp/api/config/v1alpha1"
	"github.com/cube-idp/cube-idp/internal/ref"
)

// RenderOptions carries the render inputs that come from the setup rather
// than from the pack itself.
type RenderOptions struct {
	// Values are validated against the pack's #Values definition. They are
	// meaningful only to helm and kustomize packs; a raw pack rejects them.
	Values map[string]any
	// InstanceID is the effective identity this copy of the pack is called
	// by, and it becomes the name of every object a helm pack renders.
	// Empty means artifact mode — no setup — and the pack's own name is
	// used, which is what an effective id defaults to anyway.
	InstanceID string
}

// RenderPlan is the result of rendering one pack: the objects the pack itself
// produces, plus the prerequisite objects declared beside it.
//
// The two groups are separate because they become separate delivery units:
// prerequisites need their own readiness gate before the pack's own objects
// are reconciled. Render fills Objects; RenderInstance adds the external
// manifests the setup declares beside the pack, splitting them by lifecycle.
type RenderPlan struct {
	// Prerequisites are the lifecycle:pre external manifests. They are
	// carried as data only; their delivery semantics are the delivery
	// milestone's contract.
	Prerequisites []*unstructured.Unstructured
	// Objects are the pack's rendered objects, followed by the
	// lifecycle:with external manifests when an instance declares any.
	Objects []*unstructured.Unstructured
}

// resolveDocumentFunc resolves a reference to the bytes of a single document.
//
// It is this package's seam onto internal/ref: production passes
// resolveDocument, a test passes its own. The seam is a parameter rather than
// package state, so nothing is saved and restored around a test (CLAUDE.md §2)
// and rendering stays a function of its inputs.
type resolveDocumentFunc func(ctx context.Context, reference string) ([]byte, error)

// resolveDocument is the production resolver: internal/ref in single-document
// mode. The Pin that resolution records is dropped here because nothing
// consumes pins yet; recording them belongs to the milestone that delivers.
func resolveDocument(ctx context.Context, reference string) ([]byte, error) {
	file, err := ref.ResolveFile(ctx, reference)
	if err != nil {
		return nil, err
	}
	return file.Bytes(), nil
}

// RenderInstance renders one pack instance: the setup's values applied to the
// pack, and the external manifests declared beside it attached to the plan.
//
// It is the whole of what a setup entry means for one already-loaded pack, and
// it is what the CLI edge and the later delivery milestones call. Resolution of
// valuesRef and of external refs goes through internal/ref; the pack's own
// payload was resolved before Load.
func RenderInstance(ctx context.Context, p *Pack, spec v1alpha1.PackSpec) (RenderPlan, error) {
	return renderInstance(ctx, p, spec, resolveDocument)
}

// renderInstance is RenderInstance with the resolver injected.
//
// The raw-pack check reads the spec rather than the merged values, and runs
// before anything is resolved: type is declared, so "values on a raw pack" is
// knowable without a fetch. Checking the merged map instead would fetch first
// and then miss the case entirely, because a valuesRef holding an empty mapping
// merges to no values at all.
func renderInstance(ctx context.Context, p *Pack, spec v1alpha1.PackSpec, resolve resolveDocumentFunc) (RenderPlan, error) {
	if err := ctx.Err(); err != nil {
		return RenderPlan{}, err
	}
	if p.meta.Type == TypeRaw && (spec.ValuesRef != "" || spec.Values != nil) {
		return RenderPlan{}, newValuesOnRawPackError()
	}

	values, err := instanceValues(ctx, spec, resolve)
	if err != nil {
		return RenderPlan{}, err
	}
	plan, err := p.Render(ctx, RenderOptions{Values: values, InstanceID: spec.ID})
	if err != nil {
		return RenderPlan{}, err
	}
	return p.attachExternal(ctx, plan, spec.ExternalManifests, resolve)
}

// attachExternal resolves the external manifests and places them in the plan:
// prerequisites in their own group, the rest after the pack's own objects.
//
// They get the same namespace transform the pack's objects got, per group and
// before they join the plan. An external manifest is delivered as part of this
// instance, so a pack that forces a namespace forces it over everything it
// delivers — and an object insisting on a different namespace is the same
// CUBE-PKG-008 conflict, not a silent override.
//
// Scope comes from the same index the pack's own objects were judged by,
// rebuilt from those objects: the pack's payload is the self-contained
// artifact, and a definition bundled beside it as an external manifest is not
// part of it. Rebuilding rather than threading the index through RenderPlan
// keeps that type the contract's shape; the input is identical, so the answer
// is too.
func (p *Pack) attachExternal(
	ctx context.Context,
	plan RenderPlan,
	entries []v1alpha1.ExternalManifest,
	resolve resolveDocumentFunc,
) (RenderPlan, error) {
	groups, err := resolveExternalManifests(ctx, entries, resolve)
	if err != nil {
		return RenderPlan{}, err
	}
	scopes := indexCRDScopes(plan.Objects)
	for _, group := range [][]*unstructured.Unstructured{groups.pre, groups.with} {
		if err := applyNamespace(group, p.meta.Namespace, scopes); err != nil {
			return RenderPlan{}, err
		}
	}
	plan.Prerequisites = groups.pre
	plan.Objects = append(plan.Objects, groups.with...)
	return plan, nil
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
	case TypeHelm:
		return p.planHelm(p.instanceID(opts.InstanceID), values)
	case TypeKustomize:
		return p.planKustomize(ctx, values)
	default:
		// Unreachable: #Pack admits three types and a Pack only exists by
		// way of Load. Kept total and reported as what it would be — the
		// schema and this switch having drifted apart — not a panic.
		return RenderPlan{}, newSchemaDriftError(fmt.Sprintf("unknown type %q", p.meta.Type))
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
	if err := applyNamespace(objs, p.meta.Namespace, indexCRDScopes(objs)); err != nil {
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
	if err := applyNamespace(objs, p.meta.Namespace, indexCRDScopes(objs)); err != nil {
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

// parseManifest splits one multi-document YAML file into objects.
func parseManifest(name string, data []byte) ([]*unstructured.Unstructured, error) {
	docs, err := decodeDocuments(data)
	if err != nil {
		return nil, newManifestParseError(name, err)
	}
	objs := make([]*unstructured.Unstructured, 0, len(docs))
	for _, doc := range docs {
		objs = append(objs, &unstructured.Unstructured{Object: doc})
	}
	return objs, nil
}

// decodeDocuments splits one multi-document YAML stream into mappings,
// skipping empty documents (a trailing separator is not an error).
//
// The error returned is the decoder's own, deliberately uncoded: the same
// stream means different things to the layers that read it — a manifest file,
// a values document, an external manifest — and each wraps this in the code it
// owns rather than inheriting one from here.
func decodeDocuments(data []byte) ([]map[string]any, error) {
	dec := utilyaml.NewYAMLOrJSONDecoder(bytes.NewReader(data), 4096)
	var docs []map[string]any
	for {
		doc := map[string]any{}
		if err := dec.Decode(&doc); err != nil {
			if errors.Is(err, io.EOF) {
				return docs, nil
			}
			return nil, err
		}
		if len(doc) == 0 {
			continue
		}
		docs = append(docs, doc)
	}
}
