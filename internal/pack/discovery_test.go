package pack

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
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
	if len(cols) < 4 { // VERSION, URL, AUTH-SECRET, READY (NAME is implicit)
		t.Fatalf("printer columns missing: %v", cols)
	}
}

func TestPackObjectShape(t *testing.T) {
	p := &Pack{Name: "gitea", Version: "0.1.0", Expose: &Expose{
		URLs:          []string{"https://gitea.${GATEWAY_HOST}"},
		AuthSecretRef: &SecretRef{Namespace: "gitea", Name: "gitea-admin"},
		ImpliedFields: map[string]string{"username": "gitea_admin"},
	}}
	o := PackObject(p, "cube-idp.localtest.me", true)
	if o.GetKind() != "Pack" || o.GetName() != "gitea" || o.GetNamespace() != "" {
		t.Fatalf("Pack object identity: %s %s/%s", o.GetKind(), o.GetNamespace(), o.GetName())
	}
	url, _, _ := unstructured.NestedString(o.Object, "spec", "url")
	if url != "https://gitea.cube-idp.localtest.me" {
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

func TestPackObjectWithoutExpose(t *testing.T) {
	o := PackObject(&Pack{Name: "plain", Version: "0.1.0"}, "h", false)
	if o.GetName() != "plain" {
		t.Fatal("packs without expose still get a record (VERSION/READY are useful alone)")
	}
	if _, found, _ := unstructured.NestedString(o.Object, "spec", "url"); found {
		t.Fatal("no expose -> no url field")
	}
}
