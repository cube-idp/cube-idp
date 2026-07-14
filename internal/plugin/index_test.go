package plugin

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/rafpe/cube-idp/internal/diag"
)

func tgzWithBin(t *testing.T, binName, content string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	tw.WriteHeader(&tar.Header{Name: binName, Mode: 0o755, Size: int64(len(content))})
	tw.Write([]byte(content))
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func gitIndex(t *testing.T, entryYAML string) string {
	t.Helper()
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "plugins"), 0o755)
	os.WriteFile(filepath.Join(dir, "plugins", "hello.yaml"), []byte(entryYAML), 0o644)
	for _, args := range [][]string{
		{"init", "-q"}, {"add", "-A"},
		{"-c", "user.email=t@t", "-c", "user.name=t", "commit", "-qm", "index"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return dir
}

func TestInstallVerifiesShaAndInstalls(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-only")
	}
	archive := tgzWithBin(t, "cube-idp-hello", "#!/bin/sh\necho hi\n")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write(archive) }))
	t.Cleanup(srv.Close)
	sum := sha256.Sum256(archive)
	entry := fmt.Sprintf(
		"name: hello\nshortDescription: test\nplatforms:\n  - os: %s\n    arch: %s\n    url: %s/a.tgz\n    sha256: %s\n    bin: cube-idp-hello\n",
		runtime.GOOS, runtime.GOARCH, srv.URL, hex.EncodeToString(sum[:]))
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_DATA_HOME", "")

	if err := Install(context.Background(), gitIndex(t, entry), "hello"); err != nil {
		t.Fatal(err)
	}
	p, ok := Lookup("hello")
	if !ok {
		t.Fatal("installed plugin not discoverable")
	}
	if err := EnsureTrusted("hello", p, false); err != nil {
		t.Fatalf("verified install must be pre-trusted: %v", err)
	}
}

func TestInstallRejectsShaMismatch(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-only")
	}
	archive := tgzWithBin(t, "cube-idp-hello", "#!/bin/sh\necho evil\n")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write(archive) }))
	t.Cleanup(srv.Close)
	entry := fmt.Sprintf(
		"name: hello\nshortDescription: test\nplatforms:\n  - os: %s\n    arch: %s\n    url: %s/a.tgz\n    sha256: %s\n    bin: cube-idp-hello\n",
		runtime.GOOS, runtime.GOARCH, srv.URL, "deadbeef"+string(bytes.Repeat([]byte("0"), 56)))
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_DATA_HOME", "")

	err := Install(context.Background(), gitIndex(t, entry), "hello")
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != "CUBE-7102" {
		t.Fatalf("want CUBE-7102 on sha mismatch, got %v", err)
	}
	if _, ok := Lookup("hello"); ok {
		t.Fatal("a sha-mismatched plugin must never land in InstallDir")
	}
}

func TestInstallUnknownPlugin(t *testing.T) {
	err := Install(context.Background(), gitIndex(t, "name: hello\nplatforms: []\n"), "absent")
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != "CUBE-7101" {
		t.Fatalf("want CUBE-7101, got %v", err)
	}
}
