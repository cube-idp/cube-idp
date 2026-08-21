package pack

import (
	"context"
	"io/fs"
	"path"
	"slices"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/kustomize/api/krusty"
	"sigs.k8s.io/kustomize/api/types"
	"sigs.k8s.io/kustomize/kyaml/filesys"
	"sigs.k8s.io/yaml"
)

// This file is the ONLY importer of sigs.k8s.io/kustomize — the heavy SDK
// stays out of every other build path in the domain (ARCHITECTURE §8).

// kustomizationNames are the file names kustomize accepts for a kustomization
// root, in the order it looks for them.
var kustomizationNames = []string{"kustomization.yaml", "kustomization.yml", "Kustomization"}

// renderKustomize builds the pack's kustomization into objects.
//
// The payload is copied into an in-memory filesystem, so the build reads
// nothing from the real filesystem. Remote references are rejected before the
// build starts — see checkSelfContained, which is what keeps rendering
// hermetic.
func (p *Pack) renderKustomize(ctx context.Context) ([]*unstructured.Unstructured, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := checkSelfContained(p.fsys); err != nil {
		return nil, err
	}

	memFS, err := copyToMemFS(p.fsys)
	if err != nil {
		return nil, err
	}

	// The defaults already restrict loading to the kustomization root, keep
	// plugins to statically linked builtins, and leave helm and exec disabled.
	opts := krusty.MakeDefaultOptions()
	opts.LoadRestrictions = types.LoadRestrictionsRootOnly

	resMap, err := krusty.MakeKustomizer(opts).Run(memFS, filesys.SelfDir)
	if err != nil {
		return nil, newKustomizeBuildError(err)
	}

	// ResMap preserves the kustomization's own resource order, which is a
	// function of the payload alone — so the object order is reproducible.
	objs := make([]*unstructured.Unstructured, 0, resMap.Size())
	for _, res := range resMap.Resources() {
		obj, err := res.Map()
		if err != nil {
			return nil, newKustomizeBuildError(err)
		}
		objs = append(objs, &unstructured.Unstructured{Object: obj})
	}
	if len(objs) == 0 {
		return nil, newEmptyRenderError(p.meta.Name)
	}
	return objs, nil
}

// copyToMemFS mirrors the pack payload into an in-memory filesystem rooted at
// the kustomization root, because kustomize builds against its own
// filesys.FileSystem rather than an fs.FS.
func copyToMemFS(fsys fs.FS) (filesys.FileSystem, error) {
	memFS := filesys.MakeFsInMemory()

	err := fs.WalkDir(fsys, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return newFileUnreadableError(name, err)
		}
		if d.IsDir() {
			return nil
		}
		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			return newFileUnreadableError(name, err)
		}
		if err := memFS.WriteFile(filesys.SelfDir+"/"+name, data); err != nil {
			return newFileUnreadableError(name, err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return memFS, nil
}

// checkSelfContained rejects a payload whose kustomization files reference
// anything remote, before kustomize can fetch it.
//
// This is not a stylistic rule. kustomize resolves remote resources over the
// network unconditionally: krusty.Options has no switch for it,
// LoadRestrictionsRootOnly governs only local path escape, and a remote fetch
// bypasses the in-memory filesystem entirely for the real one. Rendering is
// defined as a pure function of its inputs, so a pack's payload must be
// self-contained and bases must be vendored into it.
func checkSelfContained(fsys fs.FS) error {
	return fs.WalkDir(fsys, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return newFileUnreadableError(name, err)
		}
		if d.IsDir() || !isKustomization(name) {
			return nil
		}
		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			return newFileUnreadableError(name, err)
		}
		return checkKustomizationRefs(name, data)
	})
}

func isKustomization(name string) bool {
	return slices.Contains(kustomizationNames, path.Base(name))
}

// checkKustomizationRefs decodes one kustomization file and rejects every
// remote-looking reference in it. Decoding is lenient: an unknown field is
// kustomize's business, and this check only reads the reference fields.
func checkKustomizationRefs(name string, data []byte) error {
	var k types.Kustomization
	if err := yaml.Unmarshal(data, &k); err != nil {
		return newKustomizeBuildError(err)
	}

	refs := make([]string, 0, len(k.Resources)+len(k.Components)+len(k.Crds))
	refs = append(refs, k.Resources...)
	refs = append(refs, k.Components...)
	refs = append(refs, k.Crds...)
	refs = append(refs, k.Configurations...)
	// bases is deprecated in kustomize's API but still honoured at build time,
	// so a remote base parked there would be fetched. Skipping it because the
	// field is deprecated would be exactly the false negative this scan exists
	// to prevent.
	refs = append(refs, k.Bases...) //nolint:staticcheck // deprecated, still built

	for _, patch := range k.Patches {
		refs = append(refs, patch.Path)
	}

	for _, ref := range refs {
		if isRemoteRef(ref) {
			return newRemoteRefError(name, ref)
		}
	}
	return nil
}

// isRemoteRef reports whether kustomize would resolve a reference over the
// network. kustomize's own decision helper is unexported, so this
// reimplements it — and deliberately fails CLOSED: a local path wrongly
// rejected is a clear coded error the author fixes by renaming or vendoring,
// while a remote reference wrongly allowed would silently break hermeticity.
func isRemoteRef(ref string) bool {
	ref = strings.TrimSpace(ref)
	switch {
	case ref == "":
		return false
	case strings.Contains(ref, "://"): // https://, ssh://, oci://, …
		return true
	case strings.HasPrefix(ref, "git@"), strings.HasPrefix(ref, "git::"), strings.HasPrefix(ref, "oci::"):
		return true
	case strings.Contains(ref, "?"): // ?ref=, ?timeout=, ?submodules=
		return true
	case strings.Contains(ref, ".git"):
		return true
	case strings.HasPrefix(ref, "./"), strings.HasPrefix(ref, "../"), strings.HasPrefix(ref, "/"):
		return false
	}

	// Host shorthand: github.com/org/repo, and the host//path separator form.
	if strings.Contains(ref, "//") {
		return true
	}
	first, rest, hasPath := strings.Cut(ref, "/")
	return hasPath && rest != "" && strings.Contains(first, ".") && !isYAMLName(first)
}

// isYAMLName reports whether a path segment is a manifest file rather than a
// hostname, so that a directory holding one is not mistaken for a remote host.
func isYAMLName(segment string) bool {
	switch path.Ext(segment) {
	case ".yaml", ".yml", ".json":
		return true
	default:
		return false
	}
}
