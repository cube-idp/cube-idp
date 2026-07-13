package pack

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGuardTreeStripsSymlinks(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "ok.yaml"), []byte("a: 1"), 0o644)
	if err := os.Symlink("/etc/passwd", filepath.Join(root, "evil")); err != nil {
		t.Skip("symlinks unavailable on this platform")
	}
	removed, err := GuardTree(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 1 {
		t.Fatalf("want 1 removed symlink, got %v", removed)
	}
	if _, err := os.Lstat(filepath.Join(root, "evil")); !os.IsNotExist(err) {
		t.Fatal("symlink must be gone after GuardTree")
	}
	if _, err := os.Stat(filepath.Join(root, "ok.yaml")); err != nil {
		t.Fatal("regular files must survive GuardTree")
	}
}
