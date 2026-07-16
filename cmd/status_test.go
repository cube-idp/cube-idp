package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/fluxcd/cli-utils/pkg/object"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/cube-idp/cube-idp/internal/engine"
	"github.com/cube-idp/cube-idp/internal/ui"
)

// TestPackAccessRows pins the styled-status Access source (design doc §10):
// the D11 Pack records' spec.urls, sorted by pack name; packs without urls are
// skipped; a client error yields nil (best-effort — status never fails on it).
func TestPackAccessRows(t *testing.T) {
	gitea := newPack("gitea", "", "", nil)
	_ = unstructured.SetNestedStringSlice(gitea.Object, []string{"https://gitea.cube.local:8443"}, "spec", "urls")
	noURLs := newPack("argocd", "", "", nil)
	c := newGetFakeClient(t, gitea, noURLs)

	rows := packAccessRows(context.Background(), c)
	if len(rows) != 1 || rows[0].Name != "gitea" || rows[0].URLs[0] != "https://gitea.cube.local:8443" {
		t.Fatalf("want gitea's urls only (argocd has none): %+v", rows)
	}
}

// TestRenderStatusStyledIncludesAccess checks the styled snapshot carries the
// Access section when pack URLs exist (ModeLive is the NewFor escape hatch
// that forces styled onto a bytes.Buffer).
func TestRenderStatusStyledIncludesAccess(t *testing.T) {
	defer ui.SetMode(ui.ModeStyled)
	ui.SetMode(ui.ModeLive)
	var b bytes.Buffer
	p := ui.NewFor(&b)
	renderStatusStyled(p, []engine.ComponentHealth{{Name: "flux", Ready: true}}, nil, false,
		[]ui.PackAccess{{Name: "gitea", URLs: []string{"https://gitea.cube.local:8443"}}})
	got := b.String()
	for _, want := range []string{"Components", "flux", "Access", "https://gitea.cube.local:8443"} {
		if !strings.Contains(got, want) {
			t.Fatalf("styled status missing %q:\n%s", want, got)
		}
	}
	// And with no access rows the section is omitted entirely.
	b.Reset()
	renderStatusStyled(p, nil, nil, false, nil)
	if strings.Contains(b.String(), "Access") {
		t.Fatalf("Access section must be omitted when no pack carries URLs:\n%s", b.String())
	}
}

// TestStatusPlainByteStable pins the byte-frozen plain projection (design doc
// §8 item 4): even after stage B adds the styled/JSON surfaces, a
// non-terminal writer keeps the exact phase-1 bytes — "%s %s Ready\n" per
// component, blank line, inventory count.
func TestStatusPlainByteStable(t *testing.T) {
	defer ui.SetMode(ui.ModeStyled)
	ui.SetMode(ui.ModePlain)
	var b bytes.Buffer
	p := ui.NewFor(&b) // bytes.Buffer is never a TTY -> plain
	health := []engine.ComponentHealth{
		{Name: "flux", Ready: true},
		{Name: "traefik", Ready: false, Message: "reconciling"},
	}
	renderStatusPlain(&b, p, health, nil, false)
	const want = "✔ flux Ready\n" +
		"✗ traefik reconciling\n" +
		"\n0 object(s) in inventory\n"
	if got := b.String(); got != want {
		t.Fatalf("status plain drifted:\ngot:  %q\nwant: %q", got, want)
	}
	if strings.Contains(b.String(), "\x1b[") {
		t.Fatal("plain status must emit zero ANSI escapes")
	}
}

// TestStatusJSONDocument pins the gh-style status document (design doc §10):
// one object with cube, components, inventory (objects only under --details),
// and the overall ready verdict.
func TestStatusJSONDocument(t *testing.T) {
	health := []engine.ComponentHealth{
		{Name: "flux", Ready: true},
		{Name: "traefik", Ready: false, Message: "reconciling"},
	}
	inv := []object.ObjMetadata{
		{GroupKind: schema.GroupKind{Kind: "Namespace"}, Name: "kube-system"},
		{GroupKind: schema.GroupKind{Kind: "ConfigMap"}, Namespace: "default", Name: "cm"},
	}
	var b bytes.Buffer
	if err := writeStatusJSON(&b, "dev", health, inv, true, false); err != nil {
		t.Fatal(err)
	}
	var doc statusDoc
	if err := json.Unmarshal(b.Bytes(), &doc); err != nil {
		t.Fatalf("document is not valid JSON: %v\n%s", err, b.String())
	}
	if doc.V != 1 || doc.Cube != "dev" || doc.Ready {
		t.Fatalf("head/verdict wrong: %+v", doc)
	}
	if len(doc.Components) != 2 || doc.Components[1].Name != "traefik" || doc.Components[1].Message != "reconciling" {
		t.Fatalf("components: %+v", doc.Components)
	}
	if doc.Inventory.Count != 2 {
		t.Fatalf("inventory count: %+v", doc.Inventory)
	}
	// objects are sorted Kind-first: ConfigMap before Namespace
	if len(doc.Inventory.Objects) != 2 || doc.Inventory.Objects[0].Kind != "ConfigMap" {
		t.Fatalf("objects unsorted or missing under --details: %+v", doc.Inventory.Objects)
	}
}

// TestStatusJSONDocumentNoDetails confirms objects are omitted without
// --details (count still present).
func TestStatusJSONDocumentNoDetails(t *testing.T) {
	var b bytes.Buffer
	inv := []object.ObjMetadata{{GroupKind: schema.GroupKind{Kind: "ConfigMap"}, Name: "cm"}}
	if err := writeStatusJSON(&b, "dev", nil, inv, false, true); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(b.String(), "\"objects\"") {
		t.Fatalf("objects must be omitted without --details: %s", b.String())
	}
}

func TestFormatInventory(t *testing.T) {
	// Fixed 3-object slice: one cluster-scoped, two namespaced, deliberately out of order
	inv := []object.ObjMetadata{
		{
			GroupKind: schema.GroupKind{Group: "apps", Kind: "Deployment"},
			Namespace: "default",
			Name:      "my-app",
		},
		{
			GroupKind: schema.GroupKind{Group: "", Kind: "Namespace"},
			Namespace: "", // cluster-scoped
			Name:      "kube-system",
		},
		{
			GroupKind: schema.GroupKind{Group: "", Kind: "ConfigMap"},
			Namespace: "default",
			Name:      "app-config",
		},
	}

	output := formatInventory(inv)

	// Expected: sorted by Kind, then Namespace, then Name
	// Sorted order:
	// 1. ConfigMap (default, app-config)
	// 2. Deployment (default, my-app)
	// 3. Namespace (-, kube-system)
	// Note: tabwriter formats with spaces for column alignment
	expected := "KIND       NAMESPACE NAME\n" +
		"ConfigMap  default   app-config\n" +
		"Deployment default   my-app\n" +
		"Namespace  -         kube-system\n"

	if output != expected {
		t.Errorf("formatInventory output mismatch.\nGot:\n%q\n\nExpected:\n%q", output, expected)
	}
}
