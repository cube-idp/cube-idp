package gateway_test

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/cube-idp/cube-idp/internal/gateway"
)

// TestGatewayObject pins the cube-authored Gateway across base domains:
// the fixed platform identity, the single HTTPS listener, the wildcard
// hostname derived from the domain, and the leaf Secret reference.
//
// Two assertions guard decisions rather than shape. The port is int64
// because an unstructured object may hold only JSON-native types and
// apimachinery's deep copy panics on an int — a plain equality against 443
// would pass for an int too. And allowedRoutes must be absent: route
// attachment is deliberately unstated until M12 owns route wiring, so
// adding it has to be a deliberate act that breaks this test.
func TestGatewayObject(t *testing.T) {
	cases := []struct {
		name   string
		domain string
	}{
		{"default-shaped domain", "mycube.cube.test"},
		{"single label", "cube"},
		{"many labels", "a.b.c.d.example.internal"},
		{"digits and hyphens", "cube-01.9lives-idp.test"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			obj := gateway.GatewayObject(tc.domain)

			if obj.GetAPIVersion() != "gateway.networking.k8s.io/v1" || obj.GetKind() != "Gateway" {
				t.Fatalf("object = %s %s, want gateway.networking.k8s.io/v1 Gateway", obj.GetAPIVersion(), obj.GetKind())
			}
			if obj.GetName() != gateway.Name || obj.GetNamespace() != gateway.Namespace {
				t.Errorf("identity = %s/%s, want %s/%s", obj.GetNamespace(), obj.GetName(), gateway.Namespace, gateway.Name)
			}
			class, _, _ := unstructured.NestedString(obj.Object, "spec", "gatewayClassName")
			if class != gateway.GatewayClassName {
				t.Errorf("spec.gatewayClassName = %q, want %q", class, gateway.GatewayClassName)
			}

			listener := onlyListener(t, obj)
			if got, _, _ := unstructured.NestedString(listener, "name"); got != "websecure" {
				t.Errorf("listener name = %q, want websecure", got)
			}
			if got, _, _ := unstructured.NestedString(listener, "protocol"); got != "HTTPS" {
				t.Errorf("listener protocol = %q, want HTTPS", got)
			}
			if got, _, _ := unstructured.NestedString(listener, "hostname"); got != "*."+tc.domain {
				t.Errorf("listener hostname = %q, want %q", got, "*."+tc.domain)
			}
			if port, found, _ := unstructured.NestedFieldNoCopy(listener, "port"); !found || port != any(int64(443)) {
				t.Errorf("listener port = %#v (found=%v), want int64(443)", port, found)
			}
			if mode, _, _ := unstructured.NestedString(listener, "tls", "mode"); mode != "Terminate" {
				t.Errorf("listener tls.mode = %q, want Terminate", mode)
			}
			assertLeafCertificateRef(t, listener)

			if _, found, _ := unstructured.NestedFieldNoCopy(listener, "allowedRoutes"); found {
				t.Error("listener carries allowedRoutes; route attachment is deliberately unstated until M12")
			}
		})
	}
}

// TestGatewayObjectDeepCopies asserts the emitted object survives
// apimachinery's deep copy, which is the check that actually catches a
// non-JSON-native value anywhere in the tree.
func TestGatewayObjectDeepCopies(t *testing.T) {
	gateway.GatewayObject("mycube.cube.test").DeepCopy()
	for _, obj := range gateway.PlatformObjects() {
		obj.DeepCopy()
	}
	for _, obj := range gateway.HelmPairObjects() {
		obj.DeepCopy()
	}
}

// onlyListener returns the Gateway's single listener, failing if there is
// not exactly one: a plaintext companion would serve nothing until M12.
func onlyListener(t *testing.T, obj *unstructured.Unstructured) map[string]any {
	t.Helper()
	listeners, found, err := unstructured.NestedSlice(obj.Object, "spec", "listeners")
	if err != nil || !found {
		t.Fatalf("spec.listeners: found=%v, err=%v", found, err)
	}
	if len(listeners) != 1 {
		t.Fatalf("spec.listeners = %d entries, want exactly one HTTPS listener", len(listeners))
	}
	listener, ok := listeners[0].(map[string]any)
	if !ok {
		t.Fatalf("listener 0 is %T, want a map", listeners[0])
	}
	return listener
}

// assertLeafCertificateRef checks the listener terminates TLS with the
// domain's exported leaf Secret, same-namespace so no ReferenceGrant is
// needed.
func assertLeafCertificateRef(t *testing.T, listener map[string]any) {
	t.Helper()
	refs, found, err := unstructured.NestedSlice(listener, "tls", "certificateRefs")
	if err != nil || !found {
		t.Fatalf("listener tls.certificateRefs: found=%v, err=%v", found, err)
	}
	if len(refs) != 1 {
		t.Fatalf("tls.certificateRefs = %d entries, want exactly one", len(refs))
	}
	ref, ok := refs[0].(map[string]any)
	if !ok {
		t.Fatalf("certificateRefs entry 0 is %T, want a map", refs[0])
	}
	if kind, _, _ := unstructured.NestedString(ref, "kind"); kind != "Secret" {
		t.Errorf("certificateRefs[0].kind = %q, want Secret", kind)
	}
	if name, _, _ := unstructured.NestedString(ref, "name"); name != gateway.LeafSecretName {
		t.Errorf("certificateRefs[0].name = %q, want %q", name, gateway.LeafSecretName)
	}
	if _, found, _ := unstructured.NestedFieldNoCopy(ref, "namespace"); found {
		t.Error("certificateRefs[0] names a namespace; the same-namespace reference is what avoids needing a ReferenceGrant")
	}
}
