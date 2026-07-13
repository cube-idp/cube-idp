package registry

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestManifestsParseAndTarget(t *testing.T) {
	objs, err := Manifests()
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]bool{}
	for _, o := range objs {
		kinds[o.GetKind()] = true
		if o.GetKind() != "Namespace" && o.GetNamespace() != "cube-idp-system" {
			t.Fatalf("%s/%s must live in cube-idp-system", o.GetKind(), o.GetName())
		}
	}
	for _, want := range []string{"Namespace", "Deployment", "Service"} {
		if !kinds[want] {
			t.Fatalf("missing %s in zot manifests", want)
		}
	}
}

func TestServiceExposesNodePort(t *testing.T) {
	objs, err := Manifests()
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range objs {
		if o.GetKind() != "Service" {
			continue
		}
		typ, _, _ := unstructured.NestedString(o.Object, "spec", "type")
		if typ != "NodePort" {
			t.Fatalf("zot Service must be NodePort for node-local pulls, got %q", typ)
		}
		// ParseMultiDoc decodes via k8s.io/apimachinery/pkg/util/yaml into
		// map[string]any, which represents JSON numbers as float64 (unlike
		// runtime.DefaultUnstructuredConverter's int64) — assert accordingly.
		ports, _, _ := unstructured.NestedSlice(o.Object, "spec", "ports")
		np, _, _ := unstructured.NestedFloat64(ports[0].(map[string]any), "nodePort")
		if np != float64(NodePort) {
			t.Fatalf("nodePort: %v", np)
		}
	}
}

func TestGatewayRouteShape(t *testing.T) {
	r := GatewayRoute("cube-idp.localtest.me")
	if r.GetKind() != "HTTPRoute" || r.GetNamespace() != "cube-idp-system" {
		t.Fatalf("route identity: %s/%s", r.GetKind(), r.GetNamespace())
	}
	hostnames, _, _ := unstructured.NestedStringSlice(r.Object, "spec", "hostnames")
	if len(hostnames) != 1 || hostnames[0] != "registry.cube-idp.localtest.me" {
		t.Fatalf("hostnames: %v", hostnames)
	}
}
