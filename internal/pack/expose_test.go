package pack

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/rafpe/cube-idp/internal/diag"
)

func writePack(t *testing.T, cue string) string {
	t.Helper()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "pack.cue"), []byte(cue), 0o644)
	os.MkdirAll(filepath.Join(dir, "manifests"), 0o755)
	os.WriteFile(filepath.Join(dir, "manifests", "cm.yaml"),
		[]byte("apiVersion: v1\nkind: ConfigMap\nmetadata: {name: x, namespace: default}\n"), 0o644)
	return dir
}

func TestExposeParsed(t *testing.T) {
	dir := writePack(t, `name: "gitea"
version: "0.1.0"
expose: {
	urls: ["https://gitea.${GATEWAY_HOST}"]
	authSecretRef: {namespace: "gitea", name: "gitea-admin"}
	impliedFields: {username: "gitea_admin"}
}
`)
	p, err := Fetch(context.Background(), dir, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if p.Expose == nil || len(p.Expose.URLs) != 1 ||
		p.Expose.AuthSecretRef == nil || p.Expose.AuthSecretRef.Name != "gitea-admin" ||
		p.Expose.ImpliedFields["username"] != "gitea_admin" {
		t.Fatalf("expose not parsed: %+v", p.Expose)
	}
}

func TestExposeIsOptional(t *testing.T) {
	dir := writePack(t, "name: \"plain\"\nversion: \"0.1.0\"\n")
	p, err := Fetch(context.Background(), dir, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if p.Expose != nil {
		t.Fatalf("no expose block must mean nil, got %+v", p.Expose)
	}
}

func TestExposeInvalidIsTyped(t *testing.T) {
	// authSecretRef missing its name — the CUE schema must reject it
	dir := writePack(t, `name: "bad"
version: "0.1.0"
expose: {authSecretRef: {namespace: "x"}}
`)
	_, err := Fetch(context.Background(), dir, t.TempDir())
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != "CUBE-4011" {
		t.Fatalf("want CUBE-4011, got %v", err)
	}
}
