package ref

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// TestLocalTreeContents checks that a resolved tree carries every regular
// file under the root, keyed by its slash path, and nothing else.
func TestLocalTreeContents(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, filepath.Join(root, "pack.cue"), packCUE)
	writeFixture(t, filepath.Join(root, "manifests", "deploy.yaml"), deployYAML)
	writeFixture(t, filepath.Join(root, "manifests", "extra", "svc.yaml"), deployYAML)

	got, err := ResolveTree(t.Context(), fileURL(root))
	if err != nil {
		t.Fatalf("ResolveTree(%q) error = %v, want nil", root, err)
	}

	want := []string{"manifests/deploy.yaml", "manifests/extra/svc.yaml", "pack.cue"}
	if paths := treePaths(t, got.FS()); !slices.Equal(paths, want) {
		t.Errorf("ResolveTree(%q) walked %v, want %v", root, paths, want)
	}
	data, err := fs.ReadFile(got.FS(), "pack.cue")
	if err != nil {
		t.Fatalf("ReadFile(pack.cue) error = %v, want nil", err)
	}
	if string(data) != packCUE {
		t.Errorf("ReadFile(pack.cue) = %q, want %q", data, packCUE)
	}
}

// TestLocalEmptyTreeResolves pins a deliberate division of labour: an
// empty directory is a valid resolution of a reference, not a ref-level
// error. "This produced nothing usable" is the consuming domain's call —
// CUBE-PKG-007 — and ref does not pre-empt it.
func TestLocalEmptyTreeResolves(t *testing.T) {
	root := t.TempDir()

	got, err := ResolveTree(t.Context(), fileURL(root))
	if err != nil {
		t.Fatalf("ResolveTree(%q) on an empty directory error = %v, want nil", root, err)
	}
	if paths := treePaths(t, got.FS()); len(paths) != 0 {
		t.Errorf("ResolveTree(%q) walked %v, want no files", root, paths)
	}
	if got.Pin().Digest == "" {
		t.Error("an empty tree still records a digest, want a sha256: value")
	}
}

// TestLocalRelativeForms covers the ./ and ../ spellings of the grammar,
// which resolve against the working directory.
func TestLocalRelativeForms(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, filepath.Join(dir, "pack", "pack.cue"), packCUE)
	writeFixture(t, filepath.Join(dir, "sibling", "pack.cue"), packCUE)
	t.Chdir(filepath.Join(dir, "pack"))

	tests := []struct {
		name string
		ref  string
	}{
		{"current directory", "."},
		{"current directory with prefix", "./"},
		{"parent-relative sibling", "../sibling"},
		{"child through a parent hop", "../pack"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveTree(t.Context(), tt.ref)
			if err != nil {
				t.Fatalf("ResolveTree(%q) error = %v, want nil", tt.ref, err)
			}
			want := []string{"pack.cue"}
			if paths := treePaths(t, got.FS()); !slices.Equal(paths, want) {
				t.Errorf("ResolveTree(%q) walked %v, want %v", tt.ref, paths, want)
			}
		})
	}
}

// TestLocalSymlinkInsideRootIsFollowed pins the other half of containment:
// a symlink is refused for leaving the root, not for being a symlink.
func TestLocalSymlinkInsideRootIsFollowed(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, filepath.Join(root, "base.yaml"), deployYAML)
	if err := os.Symlink(filepath.Join(root, "base.yaml"), filepath.Join(root, "alias.yaml")); err != nil {
		t.Fatalf("symlink inside %s: %v", root, err)
	}

	got, err := ResolveTree(t.Context(), fileURL(root))
	if err != nil {
		t.Fatalf("ResolveTree(%q) error = %v, want nil", root, err)
	}
	want := []string{"alias.yaml", "base.yaml"}
	if paths := treePaths(t, got.FS()); !slices.Equal(paths, want) {
		t.Errorf("ResolveTree(%q) walked %v, want %v", root, paths, want)
	}
	data, err := fs.ReadFile(got.FS(), "alias.yaml")
	if err != nil {
		t.Fatalf("ReadFile(alias.yaml) error = %v, want nil", err)
	}
	if string(data) != deployYAML {
		t.Errorf("ReadFile(alias.yaml) = %q, want %q", data, deployYAML)
	}
}

// TestLocalMissingPathIsFetchFailure checks that an absent path surfaces
// as CUBE-REF-003 with the stdlib sentinel still reachable underneath.
func TestLocalMissingPathIsFetchFailure(t *testing.T) {
	missing := fileURL(filepath.Join(t.TempDir(), "absent"))

	_, treeErr := ResolveTree(t.Context(), missing)
	requireCode(t, treeErr, CodeFetchFailed)
	if !errors.Is(treeErr, fs.ErrNotExist) {
		t.Errorf("ResolveTree(%q) error = %v, want it to wrap fs.ErrNotExist", missing, treeErr)
	}

	_, fileErr := ResolveFile(t.Context(), missing)
	requireCode(t, fileErr, CodeFetchFailed)
	if !errors.Is(fileErr, fs.ErrNotExist) {
		t.Errorf("ResolveFile(%q) error = %v, want it to wrap fs.ErrNotExist", missing, fileErr)
	}
}

// TestTreeDigestTracksContent checks the property the pin rests on: the
// digest follows the content, not the path it was read from.
func TestTreeDigestTracksContent(t *testing.T) {
	first := t.TempDir()
	writeFixture(t, filepath.Join(first, "pack.cue"), packCUE)
	second := t.TempDir()
	writeFixture(t, filepath.Join(second, "pack.cue"), packCUE)

	a, err := ResolveTree(t.Context(), fileURL(first))
	if err != nil {
		t.Fatalf("ResolveTree(%q) error = %v, want nil", first, err)
	}
	b, err := ResolveTree(t.Context(), fileURL(second))
	if err != nil {
		t.Fatalf("ResolveTree(%q) error = %v, want nil", second, err)
	}
	if err := b.Pin().Verify(a.Pin()); err != nil {
		t.Errorf("two trees with identical content disagree: %v", err)
	}

	writeFixture(t, filepath.Join(second, "pack.cue"), packCUE+"category: \"gateway\"\n")
	edited, err := ResolveTree(t.Context(), fileURL(second))
	if err != nil {
		t.Fatalf("ResolveTree(%q) after edit error = %v, want nil", second, err)
	}
	requireCode(t, edited.Pin().Verify(a.Pin()), CodePinMismatch)
}

// treePaths walks a resolved tree and returns every file path it holds.
func treePaths(t *testing.T, fsys fs.FS) []string {
	t.Helper()

	var paths []string
	err := fs.WalkDir(fsys, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			paths = append(paths, name)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WalkDir over the resolved tree: %v", err)
	}
	return paths
}
