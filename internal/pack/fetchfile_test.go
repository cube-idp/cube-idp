package pack

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/cube-idp/cube-idp/internal/diag"
)

func writeTestFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func diagCode(t *testing.T, err error) diag.Code {
	t.Helper()
	var de *diag.Error
	if !errors.As(err, &de) {
		t.Fatalf("expected *diag.Error, got %T: %v", err, err)
	}
	return de.Code
}

func TestFetchFileLocalFile(t *testing.T) {
	dir := t.TempDir()
	p := writeTestFile(t, dir, "base.yaml", "kind: Cluster\n")
	got, err := FetchFile(context.Background(), p, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "kind: Cluster\n" {
		t.Fatalf("got %q", got)
	}
}

func TestFetchFileLocalDirSingleYAML(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "cfg.yaml", "a: 1\n")
	writeTestFile(t, dir, "README.md", "not yaml\n")
	got, err := FetchFile(context.Background(), dir, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "a: 1\n" {
		t.Fatalf("got %q", got)
	}
}

func TestFetchFileLocalDirAmbiguous(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "one.yaml", "a: 1\n")
	writeTestFile(t, dir, "two.yml", "b: 2\n")
	_, err := FetchFile(context.Background(), dir, t.TempDir())
	if c := diagCode(t, err); c != diag.CodePackFetchFail {
		t.Fatalf("code = %s, want %s", c, diag.CodePackFetchFail)
	}
}

func TestFetchFileLocalDirEmpty(t *testing.T) {
	_, err := FetchFile(context.Background(), t.TempDir(), t.TempDir())
	if c := diagCode(t, err); c != diag.CodePackFetchFail {
		t.Fatalf("code = %s, want %s", c, diag.CodePackFetchFail)
	}
}

func TestFetchFileMissingPath(t *testing.T) {
	_, err := FetchFile(context.Background(), filepath.Join(t.TempDir(), "nope.yaml"), t.TempDir())
	if c := diagCode(t, err); c != diag.CodePackFetchFail {
		t.Fatalf("code = %s, want %s", c, diag.CodePackFetchFail)
	}
}

func TestFetchFileBadScheme(t *testing.T) {
	_, err := FetchFile(context.Background(), "ftp://example.com/x.yaml", t.TempDir())
	if c := diagCode(t, err); c != diag.CodePackRefInvalid {
		t.Fatalf("code = %s, want %s", c, diag.CodePackRefInvalid)
	}
}
