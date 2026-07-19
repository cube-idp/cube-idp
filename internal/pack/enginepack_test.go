package pack

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cube-idp/cube-idp/internal/config"
	"github.com/cube-idp/cube-idp/internal/diag"
)

func TestVerifyEnginePackRef(t *testing.T) {
	ok := &Pack{Name: "cube-engine-flux"}
	if err := VerifyEnginePackRef(ok, config.EngineSpec{Type: "flux"}); err != nil {
		t.Fatal(err)
	}
	err := VerifyEnginePackRef(ok, config.EngineSpec{Type: "argocd", Ref: "/x"})
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != diag.CodeEnginePackMismatch {
		t.Fatalf("want CUBE-0013, got %v", err)
	}
	if !strings.Contains(de.Summary, "cube-engine-argocd") {
		t.Fatalf("summary must name the required pack: %q", de.Summary)
	}
}

// TestFetchRenderEngine drives the whole helper against an on-disk fixture
// pack (manifests-only is fine — values are nil here; chart rendering is
// fenced by tests/packs_render_test.go against the real packs).
func TestFetchRenderEngine(t *testing.T) {
	dir := t.TempDir()
	pd := filepath.Join(dir, "cube-engine-flux")
	os.MkdirAll(filepath.Join(pd, "manifests"), 0o755)
	os.WriteFile(filepath.Join(pd, "pack.cue"),
		[]byte("name: \"cube-engine-flux\"\nversion: \"0.1.0\"\n"), 0o644)
	os.WriteFile(filepath.Join(pd, "manifests", "ns.yaml"),
		[]byte("apiVersion: v1\nkind: Namespace\nmetadata:\n  name: flux-system\n"), 0o644)

	pk, r, err := FetchRenderEngine(context.Background(),
		config.EngineSpec{Type: "flux", Ref: pd}, config.GatewaySpec{}, pd, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if pk.Name != "cube-engine-flux" || len(r.Objects) != 1 || r.Objects[0].GetKind() != "Namespace" {
		t.Fatalf("unexpected render: %+v / %+v", pk, r.Objects)
	}
	// Wrong engine type against the same dir → CUBE-0013, no render.
	_, _, err = FetchRenderEngine(context.Background(),
		config.EngineSpec{Type: "argocd", Ref: pd}, config.GatewaySpec{}, pd, t.TempDir())
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != diag.CodeEnginePackMismatch {
		t.Fatalf("want CUBE-0013, got %v", err)
	}
}
