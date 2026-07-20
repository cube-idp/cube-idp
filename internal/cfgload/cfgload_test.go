package cfgload

import (
	"context"
	"errors"
	"io"
	"log"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-containerregistry/pkg/registry"

	"github.com/cube-idp/cube-idp/internal/diag"
	"github.com/cube-idp/cube-idp/internal/oci"
)

const doc = "apiVersion: cube-idp.dev/v1alpha1\nkind: Cube\nmetadata: {name: demo}\nspec:\n  cluster: {provider: kind}\n  engine: {type: flux}\n  gateway: {}\n"

// codeOf is the repo idiom for asserting a diag code (there is no
// diag.HasCode); mirrors internal/config/load_test.go:16.
func codeOf(t *testing.T, err error) diag.Code {
	t.Helper()
	var de *diag.Error
	if !errors.As(err, &de) {
		t.Fatalf("want *diag.Error, got %T: %v", err, err)
	}
	return de.Code
}

func TestLoadLocalFileWins(t *testing.T) {
	// A name that PARSES as a bare-git ref but exists on disk must load
	// locally (stat wins — the configs.d/cube.yaml ambiguity).
	dir := t.TempDir()
	sub := filepath.Join(dir, "configs.d")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(sub, "cube.yaml")
	if err := os.WriteFile(f, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(context.Background(), f)
	if err != nil {
		t.Fatal(err)
	}
	if c.Origin().Remote {
		t.Fatal("local file mis-classified as remote")
	}
}

func TestLoadMissingLocalNonRefIsConfigRead(t *testing.T) {
	_, err := Load(context.Background(), filepath.Join(t.TempDir(), "absent.yaml"))
	if err == nil || codeOf(t, err) != diag.CodeConfigRead {
		t.Fatalf("err = %v, want CUBE-0001", err)
	}
}

func TestLoadRemoteRefFetchFailureIsCUBE0015(t *testing.T) {
	// Unpinned bare-git remote ref: rejected inside the fetch machinery;
	// cfgload wraps as CUBE-0015 (cause chain keeps CUBE-4007).
	_, err := Load(context.Background(), "github.com/acme/cubes//prod")
	if err == nil || codeOf(t, err) != diag.CodeConfigRemoteFetch {
		t.Fatalf("err = %v, want CUBE-0015", err)
	}
	// The cause chain keeps the underlying CUBE-4007 (unpinned git ref).
	var de *diag.Error
	if !errors.As(err, &de) {
		t.Fatalf("want *diag.Error, got %T", err)
	}
	var cause *diag.Error
	if !errors.As(de.Cause, &cause) || cause.Code != diag.CodePackRefUnpin {
		t.Fatalf("cause = %v, want CUBE-4007", de.Cause)
	}
}

func TestLoadRemoteOCISetsOrigin(t *testing.T) {
	// Full remote path — fetch, LoadBytes, origin marked with a pin —
	// against an in-process OCI registry (the internal/pack catalog_test
	// fixture idiom; go-getter's http getter cannot serve a plain file in
	// ClientModeDir, see FINDINGS).
	t.Setenv("HOME", t.TempDir()) // DefaultCacheDir must be a throwaway
	srv := httptest.NewServer(registry.New(registry.Logger(log.New(io.Discard, "", 0))))
	t.Cleanup(srv.Close)
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "cube.yaml"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	ref := "oci://" + u.Host + "/cubes/demo:1.0.0"
	if _, err := oci.PushPackDir(context.Background(), dir, ref); err != nil {
		t.Fatalf("pushing cube fixture: %v", err)
	}
	c, err := Load(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	o := c.Origin()
	if !o.Remote || o.Pin == "" || o.Ref != ref {
		t.Fatalf("origin = %+v", o)
	}
	if c.Metadata.Name != "demo" {
		t.Fatalf("metadata.name = %q, want demo", c.Metadata.Name)
	}
}
