package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/fluxcd/cli-utils/pkg/object"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/cube-idp/cube-idp/internal/diag"
	"github.com/cube-idp/cube-idp/internal/engine"
	"github.com/cube-idp/cube-idp/internal/ui"
)

// stubStatusConnect swaps the cluster-connection seam for a fake whose
// collector returns the given snapshots in order (repeating the last one),
// so watch loops run entirely off-cluster (trust.go's trustInstall pattern).
func stubStatusConnect(t *testing.T, snaps ...statusSnapshot) {
	t.Helper()
	restore := statusConnect
	calls := 0
	statusConnect = func(context.Context, string, bool) (string, statusCollector, error) {
		return "watch-fixture", func(context.Context) (statusSnapshot, error) {
			i := calls
			if i >= len(snaps) {
				i = len(snaps) - 1
			}
			calls++
			return snaps[i], nil
		}, nil
	}
	t.Cleanup(func() { statusConnect = restore })
}

// TestWatchExitsWhenAllReady is the W2.T12 core semantic (spec WP7):
// --watch re-renders the one-shot view every interval and exits 0 once
// every component reports Ready — the fake collector is unready once, then
// ready, so the output must contain both renders.
func TestWatchExitsWhenAllReady(t *testing.T) {
	stubStatusConnect(t,
		statusSnapshot{Health: []engine.ComponentHealth{{Name: "flux", Ready: false, Message: "reconciling"}}},
		statusSnapshot{Health: []engine.ComponentHealth{{Name: "flux", Ready: true}}},
	)

	var out bytes.Buffer
	done := make(chan error, 1)
	go func() {
		root := NewRootCmd()
		root.SetOut(&out)
		root.SetErr(&out)
		root.SetIn(&bytes.Buffer{})
		root.SetArgs([]string{"status", "--watch", "--interval=10ms"})
		done <- root.ExecuteContext(context.Background())
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("watch must exit 0 once all components are Ready: %v\noutput: %s", err, out.String())
		}
		got := out.String()
		if !strings.Contains(got, "✗ flux reconciling") {
			t.Fatalf("expected the first (unready) render, got:\n%s", got)
		}
		if !strings.Contains(got, "✔ flux Ready") {
			t.Fatalf("expected the second (ready) render, got:\n%s", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("status --watch must exit once every component is Ready, not keep polling")
	}
}

// TestWatchInterruptedWhileUnhealthy pins the --exit-status contract: an
// interrupt (context cancel) while components are unhealthy exits 1 via the
// T08 bare sentinel — and exits 0 without the flag. Never hangs.
func TestWatchInterruptedWhileUnhealthy(t *testing.T) {
	cases := []struct {
		name     string
		args     []string
		wantExit bool
	}{
		{"exit-status", []string{"status", "--watch", "--interval=10ms", "--exit-status"}, true},
		{"default", []string{"status", "--watch", "--interval=10ms"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stubStatusConnect(t, statusSnapshot{Health: []engine.ComponentHealth{
				{Name: "flux", Ready: false, Message: "reconciling"},
			}})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() {
				root := NewRootCmd()
				var out bytes.Buffer
				root.SetOut(&out)
				root.SetErr(&out)
				root.SetIn(&bytes.Buffer{})
				root.SetArgs(tc.args)
				done <- root.ExecuteContext(ctx)
			}()
			time.AfterFunc(50*time.Millisecond, cancel)
			select {
			case err := <-done:
				var es exitStatus
				if tc.wantExit {
					if !errors.As(err, &es) || es.code != 1 {
						t.Fatalf("interrupt while unhealthy with --exit-status must be the bare exit-1 sentinel, got %v", err)
					}
					return
				}
				if err != nil {
					t.Fatalf("interrupted watch without --exit-status must exit 0, got %v", err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("interrupted watch must return promptly, not hang")
			}
		})
	}
}

// TestStatusCompactHidesReadyRows: --compact hides Ready rows in the human
// render while the overall verdict (the CUBE-coded unready error, the JSON
// ready field) still reflects the full component set.
func TestStatusCompactHidesReadyRows(t *testing.T) {
	stubStatusConnect(t, statusSnapshot{Health: []engine.ComponentHealth{
		{Name: "flux", Ready: true},
		{Name: "traefik", Ready: false, Message: "reconciling"},
	}})

	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetIn(&bytes.Buffer{})
	root.SetArgs([]string{"status", "--compact"})
	err := root.ExecuteContext(context.Background())
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != diag.CodeEngineHealthTimeout {
		t.Fatalf("unready status must keep its coded verdict under --compact, got %v", err)
	}
	got := out.String()
	if strings.Contains(got, "flux") {
		t.Fatalf("--compact must hide Ready component rows, got:\n%s", got)
	}
	if !strings.Contains(got, "✗ traefik reconciling") {
		t.Fatalf("--compact must keep unready rows, got:\n%s", got)
	}
}

// TestWatchModelLifecycle drives the TTY tick model directly (the live
// renderer's model-driven style — no PTY): an unready snapshot updates the
// region view and schedules the next tick; a ready snapshot quits through
// the scrollback-persisting final; an interrupt flags and quits.
func TestWatchModelLifecycle(t *testing.T) {
	m := watchModel{interval: time.Millisecond}

	mod, cmd := m.Update(watchSnapMsg{view: "one", allReady: false})
	wm := mod.(watchModel)
	if wm.view != "one" || cmd == nil {
		t.Fatalf("unready snap must update the view and schedule a tick, got view=%q cmd=%v", wm.view, cmd)
	}

	mod, cmd = wm.Update(watchSnapMsg{view: "two", allReady: true})
	wm = mod.(watchModel)
	if !wm.allReady || wm.view != "" || cmd == nil {
		t.Fatalf("ready snap must collapse the region and quit via the println sequence, got %+v", wm)
	}

	mod, cmd = m.Update(tea.InterruptMsg{})
	wm = mod.(watchModel)
	if !wm.interrupted || cmd == nil {
		t.Fatal("interrupt must set the interrupted flag and quit")
	}
}

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
	renderStatusStyled(&b, p, []engine.ComponentHealth{{Name: "flux", Ready: true}}, nil, false,
		[]ui.PackAccess{{Name: "gitea", URLs: []string{"https://gitea.cube.local:8443"}}})
	got := b.String()
	for _, want := range []string{"Components", "flux", "Access", "https://gitea.cube.local:8443"} {
		if !strings.Contains(got, want) {
			t.Fatalf("styled status missing %q:\n%s", want, got)
		}
	}
	// And with no access rows the section is omitted entirely.
	b.Reset()
	renderStatusStyled(&b, p, nil, nil, false, nil)
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
