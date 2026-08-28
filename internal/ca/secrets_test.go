package ca_test

import (
	"encoding/base64"
	"reflect"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/cube-idp/cube-idp/internal/ca"
)

const testNamespace = "gateway-system"

// caSecretName and leafSecretName are the gateway contract's expected
// platform-fact values (docs/domains/gateway.md) — this domain takes
// them as injected SecretPlacement fields, so tests supply them like
// the edge does.
const (
	caSecretName   = "cube-idp-ca"
	leafSecretName = "gateway-tls"
)

// TestSecretObjects pins the applied shapes against hand-authored
// goldens: the two names, the injected namespace, the tls type, and
// data values that base64-decode to exactly the minted PEM.
func TestSecretObjects(t *testing.T) {
	caMaterial, leaf := mustMint(t, testCube, testDomain, testNow(), 1)

	got := ca.SecretObjects(
		ca.SecretPlacement{Namespace: testNamespace, CASecret: caSecretName, LeafSecret: leafSecretName},
		ca.EnsureResult{CA: caMaterial, Leaf: leaf})
	want := []*unstructured.Unstructured{
		secretGolden(testNamespace, caSecretName, caMaterial),
		secretGolden(testNamespace, leafSecretName, leaf),
	}
	if len(got) != len(want) {
		t.Fatalf("SecretObjects() returned %d objects, want %d (CA first, then leaf)", len(got), len(want))
	}
	for i := range want {
		if !reflect.DeepEqual(got[i].Object, want[i].Object) {
			t.Errorf("object %d =\n%v\nwant\n%v", i, got[i].Object, want[i].Object)
		}
	}
}

// TestSecretRoundTrip is the reuse path end to end: what the edge
// applies is what the next bootstrap reads back, byte for byte.
func TestSecretRoundTrip(t *testing.T) {
	caMaterial, leaf := mustMint(t, testCube, testDomain, testNow(), 1)

	objects := ca.SecretObjects(
		ca.SecretPlacement{Namespace: testNamespace, CASecret: caSecretName, LeafSecret: leafSecretName},
		ca.EnsureResult{CA: caMaterial, Leaf: leaf})
	got, err := ca.MaterialFromSecret(objects[0])
	if err != nil {
		t.Fatalf("MaterialFromSecret() error = %v", err)
	}
	assertReused(t, got, caMaterial)
}

// TestMaterialFromSecret covers every way a Secret the edge read can
// fail to yield material — all of them the same CUBE-CA-002, with a
// remediation naming the Secret to delete.
func TestMaterialFromSecret(t *testing.T) {
	cases := []struct {
		name string
		data any
	}{
		{"no data at all", nil},
		{"tls.crt is missing", map[string]any{"tls.key": "aGk="}},
		{"tls.key is missing", map[string]any{"tls.crt": "aGk="}},
		{"tls.crt is empty", map[string]any{"tls.crt": "", "tls.key": "aGk="}},
		{"tls.key is not base64", map[string]any{"tls.crt": "aGk=", "tls.key": "not base64!"}},
		{"a data value is not a string", map[string]any{"tls.crt": int64(1), "tls.key": "aGk="}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ca.MaterialFromSecret(secretWithData(tc.data))
			coded := assertCode(t, err, ca.CodeUnusableMaterial)
			for _, want := range []string{caSecretName, testNamespace} {
				if !strings.Contains(coded.Remediation, want) {
					t.Errorf("remediation %q must name %q", coded.Remediation, want)
				}
			}
		})
	}
}

// TestUnsupportedProviderError covers the one constructor this package
// exports for the CLI edge's provider switch to raise.
func TestUnsupportedProviderError(t *testing.T) {
	coded := assertCode(t, ca.NewUnsupportedProviderError("cert-manager"), ca.CodeUnsupportedProvider)
	if !strings.Contains(coded.Summary, "cert-manager") {
		t.Errorf("summary %q must name the rejected provider", coded.Summary)
	}
	if !strings.Contains(coded.Remediation, ca.ProviderCube) {
		t.Errorf("remediation %q must name the supported provider %q", coded.Remediation, ca.ProviderCube)
	}
}

// secretGolden is the hand-authored shape SecretObjects must produce.
func secretGolden(namespace, name string, m ca.Material) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       "Secret",
		"metadata":   map[string]any{"name": name, "namespace": namespace},
		"type":       "kubernetes.io/tls",
		"data": map[string]any{
			"tls.crt": base64.StdEncoding.EncodeToString(m.CertPEM),
			"tls.key": base64.StdEncoding.EncodeToString(m.KeyPEM),
		},
	}}
}

// secretWithData builds a CA Secret carrying the given data block, or
// none at all when data is nil.
func secretWithData(data any) *unstructured.Unstructured {
	o := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       "Secret",
		"metadata":   map[string]any{"name": caSecretName, "namespace": testNamespace},
	}}
	if data != nil {
		o.Object["data"] = data
	}
	return o
}
