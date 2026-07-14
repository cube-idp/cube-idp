package syncer

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/rafpe/cube-idp/internal/diag"
)

func TestSynthesizePackFromBareDir(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "cm.yaml"),
		[]byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: x\n  namespace: default\n"), 0o644)
	p, err := loadOrSynthesize(dir)
	if err != nil {
		t.Fatal(err)
	}
	r, err := p.Render(nil)
	if err != nil || len(r.Objects) != 1 {
		t.Fatalf("render: %v (%d objects)", err, len(r.Objects))
	}
	if r.Name != filepath.Base(dir) || r.Version != "0.0.0-dev" {
		t.Fatalf("synthesized identity: %s@%s", r.Name, r.Version)
	}
}

func TestSyncRejectsEmptyDir(t *testing.T) {
	_, err := loadOrSynthesize(t.TempDir())
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != "CUBE-7201" {
		t.Fatalf("want CUBE-7201, got %v", err)
	}
}

// TestSynthesizeIgnoresNonYAMLAndSubdirs proves the *.yaml/*.yml filter and
// the "directly under dir" scope: a stray README and a nested subdirectory
// must not be staged (and must not make an otherwise-empty dir pass).
func TestSynthesizeIgnoresNonYAMLAndSubdirs(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("# not a manifest\n"), 0o644)
	os.MkdirAll(filepath.Join(dir, "sub"), 0o755)
	os.WriteFile(filepath.Join(dir, "sub", "cm.yaml"),
		[]byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: nested\n  namespace: default\n"), 0o644)

	_, err := loadOrSynthesize(dir)
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != diag.CodeSyncNoManifests {
		t.Fatalf("want CUBE-7201 (only non-manifest content present), got %v", err)
	}
}

// TestLoadOrSynthesizePrefersPackCue proves a dir WITH a pack.cue is loaded
// as a real pack (author-declared name/version), not synthesized — bare-dir
// synthesis is strictly a fallback for dirs that opted out of pack.cue.
func TestLoadOrSynthesizePrefersPackCue(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "pack.cue"), []byte(`name: "declared"
version: "1.2.3"
`), 0o644)
	os.MkdirAll(filepath.Join(dir, "manifests"), 0o755)
	os.WriteFile(filepath.Join(dir, "manifests", "cm.yaml"),
		[]byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: x\n  namespace: default\n"), 0o644)

	p, err := loadOrSynthesize(dir)
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "declared" || p.Version != "1.2.3" {
		t.Fatalf("pack.cue identity must win over synthesis: got %s@%s", p.Name, p.Version)
	}
}
