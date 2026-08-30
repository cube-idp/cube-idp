package gateway

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// TestFactsTieToContent is the substrate's fact-ties-to-content discipline,
// mirrored and extended: every exported platform fact that has an emitted
// counterpart must equal it, so a fact and the object carrying it can never
// drift apart silently. There are exactly four ties — CASecretName has no
// emitted counterpart by contract, and inventing one would be inventing
// content.
//
// It is an in-package test because the third tie is against
// implementationFQDN, which is deliberately unexported: the ExternalName
// backend is an internal detail of the stable Service's indirection.
func TestFactsTieToContent(t *testing.T) {
	objs := PlatformObjects()
	if len(objs) != 2 {
		t.Fatalf("PlatformObjects() = %d objects, want the Namespace + Service pair", len(objs))
	}
	ns, svc := objs[0], objs[1]

	if got := ns.GetName(); got != Namespace {
		t.Errorf("emitted Namespace name = %q, want the Namespace fact %q", got, Namespace)
	}
	if got := svc.GetName(); got != Name {
		t.Errorf("emitted Service name = %q, want the Name fact %q", got, Name)
	}
	external, found, err := unstructured.NestedString(svc.Object, "spec", "externalName")
	if err != nil || !found {
		t.Fatalf("emitted Service spec.externalName: found=%v, err=%v", found, err)
	}
	if external != implementationFQDN {
		t.Errorf("Service spec.externalName = %q, want the effective id's FQDN %q", external, implementationFQDN)
	}
	if got := firstCertificateRefName(t, GatewayObject("cube.test")); got != LeafSecretName {
		t.Errorf("Gateway certificateRefs[0].name = %q, want the LeafSecretName fact %q", got, LeafSecretName)
	}
}

// firstCertificateRefName digs out the emitted Gateway's single listener's
// first certificateRefs entry name.
func firstCertificateRefName(t *testing.T, obj *unstructured.Unstructured) string {
	t.Helper()
	listeners, found, err := unstructured.NestedSlice(obj.Object, "spec", "listeners")
	if err != nil || !found || len(listeners) == 0 {
		t.Fatalf("Gateway spec.listeners: found=%v, err=%v, len=%d", found, err, len(listeners))
	}
	listener, ok := listeners[0].(map[string]any)
	if !ok {
		t.Fatalf("Gateway listener 0 is %T, want a map", listeners[0])
	}
	refs, found, err := unstructured.NestedSlice(listener, "tls", "certificateRefs")
	if err != nil || !found || len(refs) == 0 {
		t.Fatalf("listener tls.certificateRefs: found=%v, err=%v, len=%d", found, err, len(refs))
	}
	ref, ok := refs[0].(map[string]any)
	if !ok {
		t.Fatalf("certificateRefs entry 0 is %T, want a map", refs[0])
	}
	name, _, _ := unstructured.NestedString(ref, "name")
	return name
}
