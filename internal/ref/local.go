package ref

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/cube-idp/cube-idp/internal/cubeerr"
)

// localTree resolves a local directory to a snapshot of its files. The
// walk is sorted and eager: it honors cancellation, enforces containment,
// and pins exactly the bytes it read.
func localTree(ctx context.Context, p parsed) (ResolvedTree, error) {
	abs, info, err := localStat(p)
	if err != nil {
		return ResolvedTree{}, err
	}
	if !info.IsDir() {
		return ResolvedTree{}, newModeMismatchError(p.ref, ModeTree, ModeFile)
	}
	files, err := walkLocalTree(ctx, p, abs)
	if err != nil {
		return ResolvedTree{}, err
	}
	return ResolvedTree{
		fsys: treeFS{files: files},
		pin:  Pin{Ref: p.ref, Source: abs, Digest: treeDigest(files)},
	}, nil
}

// localFile resolves a local path to a single file.
func localFile(ctx context.Context, p parsed) (ResolvedFile, error) {
	abs, info, err := localStat(p)
	if err != nil {
		return ResolvedFile{}, err
	}
	if info.IsDir() {
		return ResolvedFile{}, newModeMismatchError(p.ref, ModeFile, ModeTree)
	}
	if err := ctx.Err(); err != nil {
		return ResolvedFile{}, newFetchFailedError(p.ref, err)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return ResolvedFile{}, newFetchFailedError(p.ref, err)
	}
	return ResolvedFile{
		data: data,
		pin:  Pin{Ref: p.ref, Source: abs, Digest: fileDigest(data)},
	}, nil
}

// localStat turns the reference into an absolute path and stats it. The
// path the caller named is followed as written — containment governs what
// a tree may reach from its root, not which root the caller may point at.
func localStat(p parsed) (string, os.FileInfo, error) {
	abs, err := filepath.Abs(p.location)
	if err != nil {
		return "", nil, newFetchFailedError(p.ref, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", nil, newFetchFailedError(p.ref, err)
	}
	return abs, info, nil
}

// walkLocalTree reads every regular file under rootAbs. The walk runs
// inside an os.Root, so the kernel-level guarantee backs up the explicit
// containment check in visitSymlink rather than replacing it.
func walkLocalTree(ctx context.Context, p parsed, rootAbs string) (map[string][]byte, error) {
	root, err := os.OpenRoot(rootAbs)
	if err != nil {
		return nil, newFetchFailedError(p.ref, err)
	}
	defer func() { _ = root.Close() }()

	w := &localWalk{ref: p.ref, rootAbs: rootAbs, root: root, files: map[string][]byte{}}
	err = fs.WalkDir(root.FS(), ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		return w.visit(name, d)
	})
	if err != nil {
		// A containment refusal is already coded; anything else is the
		// backend failing to deliver bytes.
		var coded *cubeerr.Coded
		if errors.As(err, &coded) {
			return nil, err
		}
		return nil, newFetchFailedError(p.ref, err)
	}
	return w.files, nil
}

// localWalk carries the state of one tree walk.
type localWalk struct {
	ref     string
	rootAbs string
	root    *os.Root
	files   map[string][]byte
}

// visit records one walk entry. Directories carry no content of their own,
// and sockets, devices and pipes are not pack content.
func (w *localWalk) visit(name string, d fs.DirEntry) error {
	switch {
	case name == "." || d.IsDir():
		return nil
	case d.Type()&fs.ModeSymlink != 0:
		return w.visitSymlink(name)
	case !d.Type().IsRegular():
		return nil
	}
	data, err := w.root.ReadFile(name)
	if err != nil {
		return err
	}
	w.files[name] = data
	return nil
}

// visitSymlink admits a symlink only when its target stays inside the
// tree root. Containment is checked here, explicitly, rather than inferred
// from a backend error — that is what lets an escape always surface as
// CUBE-REF-004 and never as a generic fetch failure.
func (w *localWalk) visitSymlink(name string) error {
	target, err := filepath.EvalSymlinks(filepath.Join(w.rootAbs, name))
	if err != nil {
		return err
	}
	inside, err := w.contains(target)
	if err != nil {
		return err
	}
	if !inside {
		return newEscapesRootError(w.ref, name)
	}
	info, err := os.Stat(target)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		// A symlinked directory inside the root is already reached by its
		// real path; following it too would duplicate content.
		return nil
	}
	data, err := os.ReadFile(target)
	if err != nil {
		return err
	}
	w.files[name] = data
	return nil
}

// contains reports whether target lies inside the walk's root. Both sides
// are fully resolved first, so a root that is itself reached through a
// symlink still compares correctly.
func (w *localWalk) contains(target string) (bool, error) {
	rootReal, err := filepath.EvalSymlinks(w.rootAbs)
	if err != nil {
		return false, err
	}
	rel, err := filepath.Rel(rootReal, target)
	if err != nil {
		return false, nil
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)), nil
}
