package gateway_test

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/cube-idp/cube-idp/internal/gateway"
)

// TestPlatformObjects pins the gateway-platform unit's emission: exactly
// the Namespace and the stable Service, in apply order, both carrying
// their own namespace because they are cube-authored rather than
// pack-rendered.
//
// The ExternalName backend is spelled out here rather than derived, so a
// change to the constant expression that builds it has to be a deliberate
// edit in two places. The absent trailing dot is the assertion that
// matters most: it is the half of the trailing-dot asymmetry that
// Kubernetes requires, while the CoreDNS rewrite target requires the
// other half.
func TestPlatformObjects(t *testing.T) {
	objs := gateway.PlatformObjects()
	if len(objs) != 2 {
		t.Fatalf("PlatformObjects() = %d objects, want 2", len(objs))
	}
	ns, svc := objs[0], objs[1]

	if ns.GetKind() != "Namespace" || svc.GetKind() != "Service" {
		t.Fatalf("emission order = %s then %s, want Namespace then Service", ns.GetKind(), svc.GetKind())
	}
	if got := svc.GetNamespace(); got != gateway.Namespace {
		t.Errorf("Service metadata.namespace = %q, want %q — cube-authored objects carry their own", got, gateway.Namespace)
	}

	typ, _, _ := unstructured.NestedString(svc.Object, "spec", "type")
	if typ != "ExternalName" {
		t.Errorf("Service spec.type = %q, want ExternalName", typ)
	}
	external, _, _ := unstructured.NestedString(svc.Object, "spec", "externalName")
	const wantExternal = "traefik-gateway.gateway-system.svc.cluster.local"
	if external != wantExternal {
		t.Errorf("Service spec.externalName = %q, want %q", external, wantExternal)
	}
	if strings.HasSuffix(external, ".") {
		t.Errorf("Service spec.externalName %q ends with a dot; Kubernetes expects the relative spelling", external)
	}
}
