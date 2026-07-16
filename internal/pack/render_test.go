package pack

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/cube-idp/cube-idp/internal/config"
)

// TestRenderForSubstitutesGatewayHost pins D15 (spec D15, Owner Decisions
// #11): RenderFor extends the ${GATEWAY_HOST} expansion ExposeURLs already
// does over expose.urls to (a) chart.yaml's values: (string leaves, after
// merging pack defaults with the caller's values) and (b) manifests/*.yaml
// raw bytes, so URL-bearing packs (backstage, Task 4) can derive their
// baseUrl/hostnames from the configured gateway instead of hardcoding it.
func TestRenderForSubstitutesGatewayHost(t *testing.T) {
	p, err := Fetch(context.Background(), "testdata/gw-sub-pack", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	gw := config.GatewaySpec{Pack: "traefik", Host: "cube-idp.localtest.me", Port: 8443}
	r, err := p.RenderFor(nil, gw)
	if err != nil {
		t.Fatal(err)
	}

	var cm, route *unstructured.Unstructured
	for _, o := range r.Objects {
		switch o.GetName() {
		case "gwsub-cm":
			cm = o
		case "gwsub-route":
			route = o
		}
	}
	if cm == nil || route == nil {
		t.Fatalf("expected both gwsub-cm and gwsub-route objects, got %+v", r.Objects)
	}

	if got, _, _ := unstructured.NestedString(cm.Object, "data", "baseUrl"); got != "https://cube-idp.localtest.me:8443" {
		t.Fatalf("chart.yaml values substitution: got %q", got)
	}
	if got, _, _ := unstructured.NestedString(route.Object, "data", "host"); got != "cube-idp.localtest.me:8443" {
		t.Fatalf("manifest ${GATEWAY_HOST} substitution: got %q", got)
	}
	if got, _, _ := unstructured.NestedString(route.Object, "data", "fqdn"); got != "cube-idp.localtest.me" {
		t.Fatalf("manifest ${GATEWAY_FQDN} substitution: got %q", got)
	}
	if got, _, _ := unstructured.NestedString(route.Object, "data", "pack"); got != "traefik" {
		t.Fatalf("manifest ${GATEWAY_PACK} substitution: got %q", got)
	}
}

// TestRenderForSubstitutesGatewayPack pins F9: ${GATEWAY_PACK} expands to
// gw.Pack — the gateway pack name, which is also the namespace pack
// HTTPRoute parentRefs must target. It is exercised for BOTH pack values:
// traefik (the pre-F9 hardcoded literal, which must render byte-identically
// to before) and envoy-gateway (the case F9 fixes — routes must parent to
// ns envoy-gateway, not traefik).
func TestRenderForSubstitutesGatewayPack(t *testing.T) {
	p, err := Fetch(context.Background(), "testdata/gw-sub-pack", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, pk := range []string{"traefik", "envoy-gateway"} {
		gw := config.GatewaySpec{Pack: pk, Host: "cube-idp.localtest.me", Port: 8443}
		r, err := p.RenderFor(nil, gw)
		if err != nil {
			t.Fatal(err)
		}
		var route *unstructured.Unstructured
		for _, o := range r.Objects {
			if o.GetName() == "gwsub-route" {
				route = o
			}
		}
		if route == nil {
			t.Fatalf("%s: expected gwsub-route object", pk)
		}
		if got, _, _ := unstructured.NestedString(route.Object, "data", "pack"); got != pk {
			t.Fatalf("%s: ${GATEWAY_PACK} substitution: got %q", pk, got)
		}
	}
}

// TestRenderLeavesLiteralUntouched pins that Render (no gateway) is exactly
// RenderFor with a zero config.GatewaySpec{} — packs with no
// ${GATEWAY_HOST}/${GATEWAY_FQDN} tokens render byte-identically to before
// D15, and packs that DO have the tokens but are rendered via the
// gateway-less Render entry point see the literal text untouched rather
// than silently expanding to ":0" or similar.
func TestRenderLeavesLiteralUntouched(t *testing.T) {
	p, err := Fetch(context.Background(), "testdata/gw-sub-pack", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r, err := p.Render(nil)
	if err != nil {
		t.Fatal(err)
	}
	var route *unstructured.Unstructured
	for _, o := range r.Objects {
		if o.GetName() == "gwsub-route" {
			route = o
		}
	}
	if route == nil {
		t.Fatalf("expected gwsub-route object, got %+v", r.Objects)
	}
	if got, _, _ := unstructured.NestedString(route.Object, "data", "host"); got != "${GATEWAY_HOST}" {
		t.Fatalf("Render(nil) must leave the literal token untouched, got %q", got)
	}
}

// TestRenderForSubstitutesGatewayHostKustomize pins D15's closure of the
// kustomize-path asymmetry: RenderFor's kustomization.yaml branch now runs
// the same ${GATEWAY_HOST}/${GATEWAY_FQDN}/${GATEWAY_PACK} substitution the
// manifests/ walk and chart.yaml helm render already apply, and a zero
// GatewaySpec (the cnoe loader's RenderDir path) is untouched.
func TestRenderForSubstitutesGatewayHostKustomize(t *testing.T) {
	p, err := Fetch(context.Background(), "testdata/gw-sub-kustomize", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	gw := config.GatewaySpec{Pack: "traefik", Host: "cube-idp.localtest.me", Port: 8443}
	r, err := p.RenderFor(nil, gw)
	if err != nil {
		t.Fatal(err)
	}
	cm := r.Objects[0]
	for field, want := range map[string]string{
		"host": "cube-idp.localtest.me:8443",
		"fqdn": "cube-idp.localtest.me",
		"ns":   "traefik",
	} {
		if got, _, _ := unstructured.NestedString(cm.Object, "data", field); got != want {
			t.Fatalf("kustomize %s substitution: got %q want %q", field, got, want)
		}
	}
	// Zero-gw identity: tokens pass through untouched (the cnoe/Render path).
	r0, err := p.Render(nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, _, _ := unstructured.NestedString(r0.Objects[0].Object, "data", "host"); got != "${GATEWAY_HOST}" {
		t.Fatalf("zero-gw kustomize render must not substitute, got %q", got)
	}
}
