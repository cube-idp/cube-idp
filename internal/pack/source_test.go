package pack

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/cube-idp/cube-idp/internal/diag"
)

// tarEntry describes one entry for buildGzippedTar.
type tarEntry struct {
	name     string
	typeflag byte
	body     string
	linkname string
}

func buildGzippedTar(t *testing.T, entries []tarEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	for _, e := range entries {
		hdr := &tar.Header{
			Name:     e.name,
			Typeflag: e.typeflag,
			Mode:     0o644,
			Size:     int64(len(e.body)),
			Linkname: e.linkname,
		}
		if e.typeflag == tar.TypeDir {
			hdr.Mode = 0o755
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if e.typeflag == tar.TypeReg {
			if _, err := tw.Write([]byte(e.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func gunzipReader(t *testing.T, data []byte) *gzip.Reader {
	t.Helper()
	gr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return gr
}

// TestUntarExtractsNestedFiles covers the Flux-style gzipped-tarball layer
// path: files in nested directories land at the right paths with the right
// contents.
func TestUntarExtractsNestedFiles(t *testing.T) {
	data := buildGzippedTar(t, []tarEntry{
		{name: "pack.cue", typeflag: tar.TypeReg, body: `name: "x"`},
		{name: "manifests", typeflag: tar.TypeDir},
		{name: "manifests/deep/cm.yaml", typeflag: tar.TypeReg, body: "kind: ConfigMap"},
	})
	dest := t.TempDir()
	if err := untar(gunzipReader(t, data), dest); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{
		"pack.cue":               `name: "x"`,
		"manifests/deep/cm.yaml": "kind: ConfigMap",
	} {
		got, err := os.ReadFile(filepath.Join(dest, path))
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if string(got) != want {
			t.Fatalf("%s: got %q, want %q", path, got, want)
		}
	}
}

// TestUntarRejectsPathTraversal proves an entry escaping the destination
// (zip-slip) fails with CUBE-4012 and writes nothing outside dest.
func TestUntarRejectsPathTraversal(t *testing.T) {
	data := buildGzippedTar(t, []tarEntry{
		{name: "../escape", typeflag: tar.TypeReg, body: "pwned"},
	})
	parent := t.TempDir()
	dest := filepath.Join(parent, "dest")
	if err := os.Mkdir(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	err := untar(gunzipReader(t, data), dest)
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != "CUBE-4012" {
		t.Fatalf("want CUBE-4012, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(parent, "escape")); !os.IsNotExist(statErr) {
		t.Fatalf("traversal entry escaped the destination: %v", statErr)
	}
}

// TestUntarSkipsSymlinks proves symlink entries are silently skipped (never
// materialized) while regular files around them still extract.
func TestUntarSkipsSymlinks(t *testing.T) {
	data := buildGzippedTar(t, []tarEntry{
		{name: "link", typeflag: tar.TypeSymlink, linkname: "/etc/passwd"},
		{name: "real.txt", typeflag: tar.TypeReg, body: "ok"},
	})
	dest := t.TempDir()
	if err := untar(gunzipReader(t, data), dest); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(dest, "link")); !os.IsNotExist(err) {
		t.Fatalf("symlink entry must not be materialized: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(dest, "real.txt")); err != nil || string(got) != "ok" {
		t.Fatalf("regular file missing after symlink skip: %q %v", got, err)
	}
}

func TestSafeJoin(t *testing.T) {
	dest := t.TempDir()
	for _, rel := range []string{"a.txt", "sub/dir/f.yaml", ".", "/abs/is/reanchored"} {
		if _, err := safeJoin(dest, rel); err != nil {
			t.Fatalf("safeJoin(%q) unexpectedly rejected: %v", rel, err)
		}
	}
	for _, rel := range []string{"../x", "sub/../../x", ".."} {
		_, err := safeJoin(dest, rel)
		var de *diag.Error
		if !errors.As(err, &de) || de.Code != "CUBE-4012" {
			t.Fatalf("safeJoin(%q): want CUBE-4012, got %v", rel, err)
		}
	}
}

// TestIsLocalRegistryHost pins the ONE shared definition (Phase 4 R8) — this
// was previously duplicated byte-for-byte in internal/oci and
// internal/bundle; this table is the proof the consolidation is
// behavior-identical.
func TestIsLocalRegistryHost(t *testing.T) {
	for host, want := range map[string]bool{
		"127.0.0.1": true, "127.0.0.1:5000": true, "localhost": true,
		"localhost:30500": true, "ghcr.io": false, "ghcr.io:443": false,
		"127.0.0.1.evil.com": false, "": false,
	} {
		if got := IsLocalRegistryHost(host); got != want {
			t.Errorf("%q: got %v want %v", host, got, want)
		}
	}
}
