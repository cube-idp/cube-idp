package pack

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Local refs are the network-free slice of FetchFile's grammar; oci/git pin
// plumbing is exercised by the existing fetch tests' fixtures once the
// signature carries it through (same helpers, same seams).
func TestFetchFilePinLocalFile(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "values.yaml")
	content := []byte("replicas: 3\n")
	if err := os.WriteFile(f, content, 0o644); err != nil {
		t.Fatal(err)
	}
	b, pin, err := FetchFile(context.Background(), f, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "replicas: 3\n" {
		t.Fatalf("bytes = %q", b)
	}
	sum := sha256.Sum256(content)
	want := "file:" + hex.EncodeToString(sum[:])
	if pin != want {
		t.Fatalf("pin = %q, want %q", pin, want)
	}
}

func TestFetchFilePinLocalDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "values.yaml"), []byte("a: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, pin, err := FetchFile(context.Background(), dir, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(pin, "dir:") {
		t.Fatalf("pin = %q, want dir:<dirhash>", pin)
	}
	// Same content → same pin (dirhash is deterministic).
	_, pin2, err := FetchFile(context.Background(), dir, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if pin2 != pin {
		t.Fatalf("pin not deterministic: %q vs %q", pin2, pin)
	}
}
