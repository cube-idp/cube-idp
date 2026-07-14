package bundle

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// TestSortedImageLoads pins the deterministic ordering the ImageLoader
// providers rely on: pairs come back sorted by image ref regardless of the
// map's iteration order, each carrying its bundle tar path.
func TestSortedImageLoads(t *testing.T) {
	got := SortedImageLoads(map[string]string{
		"ghcr.io/z/img:1": "/b/images/2.tar",
		"ghcr.io/a/img:1": "/b/images/0.tar",
		"docker.io/m/x:9": "/b/images/1.tar",
	})
	want := []ImageLoad{
		{Ref: "docker.io/m/x:9", Tar: "/b/images/1.tar"},
		{Ref: "ghcr.io/a/img:1", Tar: "/b/images/0.tar"},
		{Ref: "ghcr.io/z/img:1", Tar: "/b/images/2.tar"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SortedImageLoads = %+v, want %+v", got, want)
	}
	if n := len(SortedImageLoads(nil)); n != 0 {
		t.Fatalf("SortedImageLoads(nil) len = %d, want 0", n)
	}
}

// TestPackDirLookup checks the offline pack resolver: a bundled pack (a
// packs/<name>/pack.cue that is a regular file) resolves to its dir; an
// absent pack, and a directory-only pack.cue, both report not-present so the
// caller fails loudly rather than pulling from the network.
func TestPackDirLookup(t *testing.T) {
	root := t.TempDir()
	giteaDir := filepath.Join(root, "packs", "gitea")
	if err := os.MkdirAll(giteaDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(giteaDir, "pack.cue"), []byte("package pack\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A pack.cue that is a directory (not a regular file) must not resolve.
	badDir := filepath.Join(root, "packs", "bad")
	if err := os.MkdirAll(filepath.Join(badDir, "pack.cue"), 0o755); err != nil {
		t.Fatal(err)
	}

	lookup := (&Opened{Dir: root}).PackDirLookup()

	if dir, ok := lookup("gitea"); !ok || dir != giteaDir {
		t.Fatalf("lookup(gitea) = %q,%v; want %q,true", dir, ok, giteaDir)
	}
	if _, ok := lookup("absent"); ok {
		t.Fatal("lookup(absent) reported present")
	}
	if _, ok := lookup("bad"); ok {
		t.Fatal("lookup(bad) reported present for a directory pack.cue")
	}
}
