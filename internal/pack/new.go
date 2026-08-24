package pack

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/cube-idp/cube-idp/internal/ref"
)

// namePlaceholder is what the templates below carry where the pack's name
// goes. A placeholder rather than a format verb keeps the templates readable
// as the files they become.
const namePlaceholder = "PACKNAME"

// scaffolds is the payload each pack type starts from. Every scaffold renders
// as written — that is the point of the command, and the tests assert it — so
// each carries a real object rather than a commented-out example.
//
// helm has no entry yet. Its render backend is here, but a helm pack is
// coordinates — a scaffold would have to invent a chart url and version, and
// a pack pointing at a chart that does not exist is not something an author
// can check either. Scaffolding one is --from-chart's job, in its own chunk.
var scaffolds = map[Type]map[string]string{
	TypeRaw: {
		MetadataFile: `// ` + namePlaceholder + ` — scaffolded by cube-idp.
name:    "` + namePlaceholder + `"
version: "0.1.0"
type:    "raw"

// A raw pack renders the manifests under manifests/ as they are written.
// Values have no meaning for it: supplying any is a coded error, which is
// why this pack declares no #Values.
`,
		ManifestsDir + "/configmap.yaml": `apiVersion: v1
kind: ConfigMap
metadata:
  name: ` + namePlaceholder + `
data:
  greeting: hello from ` + namePlaceholder + `
`,
	},
	TypeKustomize: {
		MetadataFile: `// ` + namePlaceholder + ` — scaffolded by cube-idp.
name:    "` + namePlaceholder + `"
version: "0.1.0"
type:    "kustomize"

// #Values is a closed definition: only the fields declared here may be
// supplied, and each one's *default fills in when it is not. A kustomize
// pack's values must be flat strings — they are substituted into ${...}
// references in the built output.
#Values: {
	greeting: string | *"hello"
}
`,
		"kustomization.yaml": `resources:
- configmap.yaml
`,
		"configmap.yaml": `apiVersion: v1
kind: ConfigMap
metadata:
  name: ` + namePlaceholder + `
data:
  greeting: ${greeting}
`,
	},
}

// NewOptions are the inputs to New.
type NewOptions struct {
	// Dir is the directory to create. It must not already exist: a pack is
	// created, never merged into whatever is already there.
	Dir string
	// Name is the pack's name. Empty takes the directory's base name, which
	// is what an author means by `pack new ./traefik` — except when forking,
	// where an empty Name keeps the name the source pack already has.
	Name string
	// Type is the type to scaffold; empty means TypeRaw. It is not consulted
	// when From is set, because a fork keeps the type its source declares.
	Type Type
	// From optionally references an existing pack to copy instead of
	// scaffolding a fresh one. It is a reference in the grammar
	// internal/ref owns, resolved as a tree.
	From string
}

// New creates a pack directory that is ready to render: either a fresh
// scaffold, or a copy of the pack From references.
//
// Everything is assembled and validated in memory first, and the directory is
// created only once the pack is known to be sound — so a rejected name or an
// unreadable source leaves nothing behind to clean up or to mistake for a
// half-made pack.
func New(ctx context.Context, opts NewOptions) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	files, err := newPackFiles(ctx, opts)
	if err != nil {
		return err
	}
	return writeTree(opts.Dir, files)
}

// newPackFiles assembles the pack's content, validated and in memory.
//
// An empty Name means different things to the two paths, and deliberately so:
// a scaffold has no name to inherit, so it takes the directory's; a fork does
// have one, and copying is what --from means — renaming happens only when it
// was asked for.
func newPackFiles(ctx context.Context, opts NewOptions) (map[string][]byte, error) {
	if opts.From != "" {
		return forkedFiles(ctx, opts.Name, opts.From)
	}
	name := opts.Name
	if name == "" {
		name = filepath.Base(opts.Dir)
	}
	packType := opts.Type
	if packType == "" {
		packType = TypeRaw
	}
	return scaffoldedFiles(name, packType)
}

// scaffoldedFiles renders the templates for one pack type and checks the
// metadata before any of it reaches the disk, exactly as the config scaffold
// does: a name the schema rejects is the usual coded error with no directory
// left behind.
func scaffoldedFiles(name string, packType Type) (map[string][]byte, error) {
	tmpl, ok := scaffolds[packType]
	if !ok {
		return nil, newScaffoldTypeUnsupportedError(packType)
	}

	files := make(map[string][]byte, len(tmpl))
	for file, body := range tmpl {
		files[file] = []byte(strings.ReplaceAll(body, namePlaceholder, name))
	}
	if _, _, err := decodeMetadata(files[MetadataFile]); err != nil {
		return nil, err
	}
	return files, nil
}

// forkedFiles resolves the source pack and copies it wholesale.
//
// The source is loaded before it is copied, so forking something that is not a
// pack fails as "that is not a pack" rather than leaving a directory of files
// the next command rejects.
func forkedFiles(ctx context.Context, name, from string) (map[string][]byte, error) {
	tree, err := ref.ResolveTree(ctx, from)
	if err != nil {
		return nil, fmt.Errorf("--from: %w", err)
	}
	if _, err := Load(ctx, tree.FS(), from); err != nil {
		return nil, err
	}

	files, err := readTree(tree.FS(), from)
	if err != nil {
		return nil, err
	}
	return renamePack(files, name, from)
}

// readTree reads every regular file of a resolved tree into memory.
func readTree(fsys fs.FS, from string) (map[string][]byte, error) {
	files := map[string][]byte{}
	err := fs.WalkDir(fsys, ".", func(name string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case d.IsDir():
			return nil
		}
		data, readErr := fs.ReadFile(fsys, name)
		if readErr != nil {
			return readErr
		}
		files[name] = data
		return nil
	})
	if err != nil {
		return nil, newForkFailedError(from, err)
	}
	return files, nil
}

// nameField matches the pack.cue name field: a top-level name whose value is a
// plain quoted string, which is what every pack this tool writes carries.
var nameField = regexp.MustCompile(`(?m)^name:(\s*)"[^"]*"$`)

// renamePack gives a forked pack its new name. An empty name is no rename at
// all: the copy keeps what its source called itself.
//
// The rewrite is textual and then verified by re-reading the metadata, because
// a source pack may spell its name in CUE this cannot safely edit — a
// reference, an interpolation, a field built by unification. Being told to
// rename it by hand is a small chore; being handed a pack quietly still called
// by its source's name is a duplicate identity in someone's setup.
func renamePack(files map[string][]byte, name, from string) (map[string][]byte, error) {
	src := files[MetadataFile]
	meta, _, err := decodeMetadata(src)
	if err != nil {
		return nil, err
	}
	if name == "" || meta.Name == name {
		return files, nil
	}

	if nameField.FindAllIndex(src, -1) == nil {
		return nil, newForkRenameError(from, name)
	}
	renamed := nameField.ReplaceAll(src, []byte(`name:${1}"`+name+`"`))

	got, _, err := decodeMetadata(renamed)
	if err != nil || got.Name != name {
		return nil, newForkRenameError(from, name)
	}
	files[MetadataFile] = renamed
	return files, nil
}

// writeTree creates dir and writes every file under it.
//
// The directory is created with Mkdir rather than MkdirAll, so an existing
// target is a refusal instead of a merge — the same reason the config scaffold
// writes O_EXCL. A failure part-way removes what was created, since a
// half-written pack directory would fail the next run as "already exists" and
// hide the real cause.
func writeTree(dir string, files map[string][]byte) error {
	if err := os.Mkdir(dir, 0o750); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return newTargetExistsError(dir)
		}
		return newScaffoldFailedError(dir, err)
	}

	for _, name := range slices.Sorted(maps.Keys(files)) {
		full := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			return cleanupTree(dir, err)
		}
		if err := os.WriteFile(full, files[name], 0o600); err != nil {
			return cleanupTree(dir, err)
		}
	}
	return nil
}

// cleanupTree removes a partially written pack and reports the write failure.
func cleanupTree(dir string, cause error) error {
	_ = os.RemoveAll(dir)
	return newScaffoldFailedError(dir, cause)
}
