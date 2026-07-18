package pack

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/cube-idp/cube-idp/internal/config"
)

func TestCRDParsesAndPrintsColumns(t *testing.T) {
	crd, err := CRD()
	if err != nil {
		t.Fatal(err)
	}
	if crd.GetKind() != "CustomResourceDefinition" || crd.GetName() != "packs.cube-idp.dev" {
		t.Fatalf("CRD identity: %s/%s", crd.GetKind(), crd.GetName())
	}
	scope, _, _ := unstructured.NestedString(crd.Object, "spec", "scope")
	if scope != "Cluster" {
		t.Fatalf("Pack must be cluster-scoped, got %q", scope)
	}
	vers, _, _ := unstructured.NestedSlice(crd.Object, "spec", "versions")
	cols, _, _ := unstructured.NestedSlice(vers[0].(map[string]any), "additionalPrinterColumns")
	if len(cols) < 5 { // VERSION, URL, AUTH-SECRET, READY, CUSTOMIZED (NAME is implicit)
		t.Fatalf("printer columns missing: %v", cols)
	}
	// U4 (GT15): customized installs are visible in `kubectl get packs`.
	var hasCustomized bool
	for _, c := range cols {
		if n, _, _ := unstructured.NestedString(c.(map[string]any), "name"); n == "CUSTOMIZED" {
			hasCustomized = true
		}
	}
	if !hasCustomized {
		t.Fatalf("CUSTOMIZED printer column missing: %v", cols)
	}
}

func TestPackObjectShape(t *testing.T) {
	p := &Pack{Name: "gitea", Version: "0.1.0", Expose: &Expose{
		URLs:          []string{"https://gitea.${GATEWAY_HOST}"},
		AuthSecretRef: &SecretRef{Namespace: "gitea", Name: "gitea-admin"},
		ImpliedFields: map[string]string{"username": "gitea_admin"},
	}}
	o := PackObject(p, config.GatewaySpec{Host: "cube-idp.localtest.me", Port: 8443}, true, false)
	if o.GetKind() != "Pack" || o.GetName() != "gitea" || o.GetNamespace() != "" {
		t.Fatalf("Pack object identity: %s %s/%s", o.GetKind(), o.GetNamespace(), o.GetName())
	}
	url, _, _ := unstructured.NestedString(o.Object, "spec", "url")
	if url != "https://gitea.cube-idp.localtest.me:8443" {
		t.Fatalf("gateway host not substituted: %q", url)
	}
	sec, _, _ := unstructured.NestedString(o.Object, "spec", "authSecret")
	if sec != "gitea/gitea-admin" {
		t.Fatalf("authSecret column value: %q", sec)
	}
	ready, _, _ := unstructured.NestedBool(o.Object, "spec", "ready")
	if !ready {
		t.Fatal("ready must be carried into the record")
	}
}

// TestPackObjectCustomized pins GT15's operator visibility (U4): a pack
// installed with non-empty values or extraManifests is CUSTOMIZED. The
// record ALWAYS carries spec.customized as "yes"/"no" — never absent — so
// `kubectl get packs` renders the column for stock packs too instead of a
// blank cell.
func TestPackObjectCustomized(t *testing.T) {
	for _, tt := range []struct {
		customized bool
		want       string
	}{{true, "yes"}, {false, "no"}} {
		o := PackObject(&Pack{Name: "p", Version: "0.1.0"}, config.GatewaySpec{Host: "h", Port: 8443}, true, tt.customized)
		got, found, _ := unstructured.NestedString(o.Object, "spec", "customized")
		if !found || got != tt.want {
			t.Fatalf("customized=%v: spec.customized = %q (found=%v), want %q", tt.customized, got, found, tt.want)
		}
	}
}

func TestPackObjectWithoutExpose(t *testing.T) {
	o := PackObject(&Pack{Name: "plain", Version: "0.1.0"}, config.GatewaySpec{Host: "h", Port: 8443}, false, false)
	if o.GetName() != "plain" {
		t.Fatal("packs without expose still get a record (VERSION/READY are useful alone)")
	}
	if _, found, _ := unstructured.NestedString(o.Object, "spec", "url"); found {
		t.Fatal("no expose -> no url field")
	}
}

// TestPackObjectGatewayPortSubstitution pins the D11 UX-hardening defect fix
// (Task 15.1): rendered URLs must carry the gateway's actual listening
// port, since the default gateway.port (8443) is never the bare host —
// the pre-fix ${GATEWAY_HOST} substitution injected only gw.Host, so
// `kubectl get packs` printed dead links (https://... with no :8443).
// Port 443 is the one exception: it's HTTPS's default, so the URL omits
// the port suffix for a cleaner, still-correct link.
func TestPackObjectGatewayPortSubstitution(t *testing.T) {
	newPack := func() *Pack {
		return &Pack{Name: "gitea", Version: "0.1.0", Expose: &Expose{
			URLs: []string{"https://gitea.${GATEWAY_HOST}"},
		}}
	}

	tests := []struct {
		name string
		gw   config.GatewaySpec
		want string
	}{
		{"non-443 port is appended", config.GatewaySpec{Host: "cube-idp.localtest.me", Port: 8443}, "https://gitea.cube-idp.localtest.me:8443"},
		{"443 is the HTTPS default, no suffix", config.GatewaySpec{Host: "cube-idp.localtest.me", Port: 443}, "https://gitea.cube-idp.localtest.me"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := PackObject(newPack(), tt.gw, true, false)
			url, _, _ := unstructured.NestedString(o.Object, "spec", "url")
			if url != tt.want {
				t.Fatalf("gw=%+v: got %q, want %q", tt.gw, url, tt.want)
			}
		})
	}
}
