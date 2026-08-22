package ref

import (
	"bytes"
	"io"
	"io/fs"
	"path"
	"slices"
	"strings"
	"time"
)

// treeFS is the read-only, in-memory filesystem a ResolvedTree hands out.
// It is a snapshot taken at resolve time: the pin recorded beside it
// describes exactly these bytes, so nothing can change underneath a
// consumer walking the tree. Only regular files are stored — directories
// are synthesized from the paths, and symlinks never survive resolution.
type treeFS struct {
	files map[string][]byte // slash-separated paths relative to the root
}

var (
	_ fs.ReadDirFS   = treeFS{}
	_ fs.ReadFileFS  = treeFS{}
	_ fs.StatFS      = treeFS{}
	_ fs.ReadDirFile = (*openTreeDir)(nil)
)

// Open implements fs.FS. Directories are synthesized from the stored
// paths, so every parent of a stored file opens without being stored.
func (t treeFS) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	if data, ok := t.files[name]; ok {
		return &openTreeFile{
			info: treeInfo{name: path.Base(name), size: int64(len(data))},
			r:    bytes.NewReader(data),
		}, nil
	}
	entries, ok := t.dirEntries(name)
	if !ok {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	return &openTreeDir{info: treeInfo{name: path.Base(name), dir: true}, entries: entries}, nil
}

// ReadFile implements fs.ReadFileFS, returning a copy so a consumer cannot
// edit the snapshot through the bytes it was handed.
func (t treeFS) ReadFile(name string) ([]byte, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "readfile", Path: name, Err: fs.ErrInvalid}
	}
	data, ok := t.files[name]
	if !ok {
		return nil, &fs.PathError{Op: "readfile", Path: name, Err: fs.ErrNotExist}
	}
	return bytes.Clone(data), nil
}

// ReadDir implements fs.ReadDirFS. Entries are sorted by name, which is
// what makes a walk over a resolved tree deterministic.
func (t treeFS) ReadDir(name string) ([]fs.DirEntry, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrInvalid}
	}
	entries, ok := t.dirEntries(name)
	if !ok {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrNotExist}
	}
	return entries, nil
}

// Stat implements fs.StatFS.
func (t treeFS) Stat(name string) (fs.FileInfo, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "stat", Path: name, Err: fs.ErrInvalid}
	}
	if data, ok := t.files[name]; ok {
		return treeInfo{name: path.Base(name), size: int64(len(data))}, nil
	}
	if _, ok := t.dirEntries(name); ok {
		return treeInfo{name: path.Base(name), dir: true}, nil
	}
	return nil, &fs.PathError{Op: "stat", Path: name, Err: fs.ErrNotExist}
}

// dirEntries lists the immediate children of dir, reporting false when no
// stored path lies under it. The root always exists, even when empty.
func (t treeFS) dirEntries(dir string) ([]fs.DirEntry, bool) {
	prefix := ""
	if dir != "." {
		prefix = dir + "/"
	}
	children := map[string]fs.DirEntry{}
	exists := dir == "."
	for p, data := range t.files {
		if !strings.HasPrefix(p, prefix) {
			continue
		}
		exists = true
		rest := p[len(prefix):]
		if base, _, nested := strings.Cut(rest, "/"); nested {
			children[base] = fs.FileInfoToDirEntry(treeInfo{name: base, dir: true})
			continue
		}
		children[rest] = fs.FileInfoToDirEntry(treeInfo{name: rest, size: int64(len(data))})
	}
	if !exists {
		return nil, false
	}
	entries := make([]fs.DirEntry, 0, len(children))
	for _, e := range children {
		entries = append(entries, e)
	}
	slices.SortFunc(entries, func(a, b fs.DirEntry) int { return strings.Compare(a.Name(), b.Name()) })
	return entries, true
}

// treeInfo is the fs.FileInfo of a snapshot entry. ModTime is deliberately
// the zero time: rendering is deterministic and must never depend on when
// a pack was fetched.
type treeInfo struct {
	name string
	size int64
	dir  bool
}

func (i treeInfo) Name() string       { return i.name }
func (i treeInfo) Size() int64        { return i.size }
func (i treeInfo) ModTime() time.Time { return time.Time{} }
func (i treeInfo) IsDir() bool        { return i.dir }
func (i treeInfo) Sys() any           { return nil }

func (i treeInfo) Mode() fs.FileMode {
	if i.dir {
		return fs.ModeDir | 0o555
	}
	return 0o444
}

// openTreeFile is one open regular file in a snapshot.
type openTreeFile struct {
	info treeInfo
	r    *bytes.Reader
}

func (f *openTreeFile) Stat() (fs.FileInfo, error) { return f.info, nil }
func (f *openTreeFile) Read(p []byte) (int, error) { return f.r.Read(p) }
func (f *openTreeFile) Close() error               { return nil }

// openTreeDir is one open directory in a snapshot.
type openTreeDir struct {
	info    treeInfo
	entries []fs.DirEntry
	offset  int
}

func (d *openTreeDir) Stat() (fs.FileInfo, error) { return d.info, nil }
func (d *openTreeDir) Close() error               { return nil }

func (d *openTreeDir) Read([]byte) (int, error) {
	return 0, &fs.PathError{Op: "read", Path: d.info.name, Err: fs.ErrInvalid}
}

// ReadDir implements fs.ReadDirFile.
func (d *openTreeDir) ReadDir(n int) ([]fs.DirEntry, error) {
	remaining := d.entries[d.offset:]
	if n <= 0 {
		d.offset = len(d.entries)
		return slices.Clone(remaining), nil
	}
	if len(remaining) == 0 {
		return nil, io.EOF
	}
	remaining = remaining[:min(n, len(remaining))]
	d.offset += len(remaining)
	return slices.Clone(remaining), nil
}
