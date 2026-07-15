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
	r := GatewayRoute("cube-idp.localtest.me", "traefik")
	if r.GetKind() != "HTTPRoute" || r.GetNamespace() != "cube-idp-system" {
		t.Fatalf("route identity: %s/%s", r.GetKind(), r.GetNamespace())
	}
	hostnames, _, _ := unstructured.NestedStringSlice(r.Object, "spec", "hostnames")
	if len(hostnames) != 1 || hostnames[0] != "registry.cube-idp.localtest.me" {
		t.Fatalf("hostnames: %v", hostnames)
	}
}

// TestGatewayRouteParentNamespace pins F9: the registry route's parentRef
// namespace tracks the gateway pack (its namespace by convention), so with
// gateway.pack=envoy-gateway the route attaches to the Gateway in ns
// envoy-gateway instead of a hardcoded "traefik" (which left Attached
// Routes at 0 and reset every TLS/HTTP connection).
func TestGatewayRouteParentNamespace(t *testing.T) {
	for _, pack := range []string{"traefik", "envoy-gateway"} {
		r := GatewayRoute("cube-idp.localtest.me", pack)
		parents, _, _ := unstructured.NestedSlice(r.Object, "spec", "parentRefs")
		if len(parents) != 1 {
			t.Fatalf("%s: want 1 parentRef, got %v", pack, parents)
		}
		p := parents[0].(map[string]any)
		if p["namespace"] != pack {
			t.Fatalf("%s: parentRef namespace = %v, want %s", pack, p["namespace"], pack)
		}
		if p["name"] != "cube-idp" {
			t.Fatalf("%s: parentRef name = %v, want cube-idp", pack, p["name"])
		}
	}
}
