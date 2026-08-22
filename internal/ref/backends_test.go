package ref

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/cube-idp/cube-idp/internal/cubeerr"
)

// Fixture content. Consts rather than vars: nothing in this package, tests
// included, keeps mutable state at package level (CLAUDE.md §2).
const (
	packCUE = `name: "demo"
version: "0.1.0"
type: "raw"
`
	deployYAML = `apiVersion: apps/v1
kind: Deployment
`
	valuesYAML = `replicas: 2
`
)

// TestBackendConformance runs the one shared behavioral suite against
// every backend in the scheme table, implemented and deferred alike.
func TestBackendConformance(t *testing.T) {
	cases := []backendCase{
		localCase(t),
		httpsCase(t),
		deferredCase("git", "git+https://example.com/org/repo.git?ref=v1&path=charts", CodeGitNotImplemented),
		deferredCase("oci", "oci://example.com/org/pack:0.1.0", CodeOCINotImplemented),
		deferredCase("s3", "s3://bucket/key/pack.yaml", CodeS3NotImplemented),
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { runBackendConformance(t, c) })
	}
}

// localCase resolves a directory and a file out of a temporary tree.
func localCase(t *testing.T) backendCase {
	t.Helper()

	dir := t.TempDir()
	treeDir := filepath.Join(dir, "pack")
	writeFixture(t, filepath.Join(treeDir, "pack.cue"), packCUE)
	writeFixture(t, filepath.Join(treeDir, "manifests", "deploy.yaml"), deployYAML)
	filePath := filepath.Join(dir, "values.yaml")
	writeFixture(t, filePath, valuesYAML)

	return backendCase{
		name:        "local",
		resolveTree: ResolveTree,
		resolveFile: ResolveFile,
		treeRef:     func(*testing.T) string { return fileURL(treeDir) },
		fileRef:     func(*testing.T) string { return fileURL(filePath) },
		fileWant:    []byte(valuesYAML),
		escapingRef: localEscapingRef,
	}
}

// localEscapingRef builds a tree holding a symlink that leaves its own
// root — the containment case every tree backend must refuse.
func localEscapingRef(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	outside := filepath.Join(dir, "outside.yaml")
	writeFixture(t, outside, "secret: true\n")

	root := filepath.Join(dir, "pack")
	writeFixture(t, filepath.Join(root, "pack.cue"), packCUE)
	if err := os.Symlink(outside, filepath.Join(root, "escape.yaml")); err != nil {
		t.Fatalf("symlink into %s: %v", root, err)
	}
	return fileURL(root)
}

// httpsCase serves one document from a real TLS server. resolveFile hands
// fetchHTTPS the client that server publishes for its own certificate, so
// the transport and the server are both real and nothing is mocked.
func httpsCase(t *testing.T) backendCase {
	t.Helper()

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(valuesYAML))
	}))
	t.Cleanup(srv.Close)
	client := srv.Client()

	return backendCase{
		name:        "https",
		resolveTree: ResolveTree,
		resolveFile: func(ctx context.Context, r string) (ResolvedFile, error) {
			p, err := parse(r)
			if err != nil {
				return ResolvedFile{}, err
			}
			return fetchHTTPS(ctx, p, client)
		},
		fileRef:  func(*testing.T) string { return srv.URL + "/values.yaml" },
		fileWant: []byte(valuesYAML),
	}
}

// deferredCase describes a backend the parser recognizes but this build
// does not carry.
func deferredCase(name, r string, code cubeerr.Code) backendCase {
	return backendCase{
		name:         name,
		resolveTree:  ResolveTree,
		resolveFile:  ResolveFile,
		treeRef:      func(*testing.T) string { return r },
		deferredCode: code,
	}
}

// writeFixture writes content at path, creating the parent directories.
func writeFixture(t *testing.T, path, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// fileURL spells an absolute path as the file:///abs form of the grammar.
func fileURL(abs string) string { return "file://" + filepath.ToSlash(abs) }
