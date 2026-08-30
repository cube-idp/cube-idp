package cli

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	v1alpha1 "github.com/cube-idp/cube-idp/api/config/v1alpha1"
	"github.com/cube-idp/cube-idp/internal/ca"
	"github.com/cube-idp/cube-idp/internal/cubeerr"
	"github.com/cube-idp/cube-idp/internal/gateway"
)

// errCluster stands in for an API failure that is not NotFound. It is a
// sentinel so the assertion is errors.Is on identity, never a message match.
var errCluster = errors.New("the API server is having a day")

// testNow anchors every minted certificate in these rows.
var testNow = time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)

// caSecretNotFound is what the API server answers for an absent Secret — and
// for a present-but-absent namespace on a first bootstrap, which is the same
// answer: mint.
func caSecretNotFound() error {
	return apierrors.NewNotFound(schema.GroupResource{Resource: "secrets"}, gateway.CASecretName)
}

// seededCASecret mints CA material and returns it beside the Secret object a
// cluster holding it would answer with — the write and read paths are one
// codec, so the fixture goes through the domain's own emitter.
func seededCASecret(t *testing.T) (ca.Material, *unstructured.Unstructured) {
	t.Helper()
	caMaterial, leaf, err := ca.Mint(ca.MintRequest{
		CubeName: "dev", Domain: testDomain, Now: testNow, Rand: rand.Reader,
	})
	if err != nil {
		t.Fatalf("ca.Mint: %v", err)
	}
	placement := ca.SecretPlacement{
		Namespace:  gateway.Namespace,
		CASecret:   gateway.CASecretName,
		LeafSecret: gateway.LeafSecretName,
	}
	objs := ca.SecretObjects(placement, ca.EnsureResult{CA: caMaterial, Leaf: leaf})
	return caMaterial, objs[0]
}

// TestEnsureCA walks the mint-if-absent contract against a hand-rolled
// reader: absence mints, usable material is reused byte for byte, and every
// other outcome surfaces with the code its owner raised.
func TestEnsureCA(t *testing.T) {
	t.Parallel()
	seeded, secret := seededCASecret(t)
	unusable := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1", "kind": "Secret",
		"metadata": map[string]any{"name": gateway.CASecretName, "namespace": gateway.Namespace},
		"data":     map[string]any{"tls.crt": "bm90LWEtY2VydA==", "tls.key": "bm90LWEta2V5"},
	}}
	cases := []struct {
		name       string
		provider   string
		obj        *unstructured.Unstructured
		readErr    error
		wantMinted bool
		wantReused bool
		wantCode   cubeerr.Code
		wantErrIs  error
	}{
		{name: "an absent secret mints", provider: ca.ProviderCube,
			readErr: caSecretNotFound(), wantMinted: true},
		{name: "usable material is reused byte for byte", provider: ca.ProviderCube,
			obj: secret, wantReused: true},
		{name: "unusable material is never silently re-minted", provider: ca.ProviderCube,
			obj: unusable, wantCode: ca.CodeUnusableMaterial},
		{name: "a read failure surfaces uncoded", provider: ca.ProviderCube,
			readErr: errCluster, wantErrIs: errCluster},
		{name: "an unsupported provider never reads", provider: "user",
			wantCode: ca.CodeUnsupportedProvider},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			reads := 0
			read := func(_ context.Context, name string) (*unstructured.Unstructured, error) {
				reads++
				if name != gateway.CASecretName {
					t.Errorf("read secret %q, want %q", name, gateway.CASecretName)
				}
				return tc.obj, tc.readErr
			}

			got, err := ensureCA(t.Context(), read, caInputs{
				provider: tc.provider, cubeName: "dev", domain: testDomain,
				now: testNow, rand: rand.Reader,
			})
			assertEnsureError(t, err, tc.wantCode, tc.wantErrIs)
			if tc.wantCode != "" || tc.wantErrIs != nil {
				if tc.provider != ca.ProviderCube && reads != 0 {
					t.Errorf("an unsupported provider performed %d reads, want none", reads)
				}
				return
			}
			if got.Minted != tc.wantMinted {
				t.Errorf("Minted = %v, want %v", got.Minted, tc.wantMinted)
			}
			if tc.wantReused && !bytes.Equal(got.CA.CertPEM, seeded.CertPEM) {
				t.Error("existing CA material was not reused byte for byte")
			}
			if len(got.Leaf.CertPEM) == 0 {
				t.Error("no leaf was minted; the leaf is minted on every bootstrap")
			}
		})
	}
}

// assertEnsureError checks the failure a row expects, by code or by
// sentinel identity — never by message.
func assertEnsureError(t *testing.T, err error, wantCode cubeerr.Code, wantIs error) {
	t.Helper()
	switch {
	case wantCode != "":
		assertCode(t, err, wantCode)
	case wantIs != nil:
		if !errors.Is(err, wantIs) {
			t.Fatalf("err = %v, want it to wrap %v", err, wantIs)
		}
		var coded *cubeerr.Coded
		if errors.As(err, &coded) {
			t.Fatalf("the edge tagged its own read failure %s; the CLI originates no codes", coded.Code)
		}
	case err != nil:
		t.Fatalf("ensureCA: %v", err)
	}
}

// TestEnsureCAMaterialSkipsWithoutTheUnit: a list that drops ca-secrets must
// neither read nor mint — a cube without the gateway fabric is constructible
// by explicit choice.
func TestEnsureCAMaterialSkipsWithoutTheUnit(t *testing.T) {
	t.Parallel()
	cfg := &v1alpha1.Config{Spec: v1alpha1.ConfigSpec{
		Prerequisites: []v1alpha1.PrerequisiteSpec{{Name: v1alpha1.PrerequisiteGatewayPlatform}},
	}}
	cfg.Name = "dev"

	// A nil dynamic client is the assertion: reaching the cluster panics.
	got, err := ensureCAMaterial(t.Context(), nil, cfg, testDomain, defaultBootstrapDeps())
	if err != nil {
		t.Fatalf("ensureCAMaterial: %v", err)
	}
	if got.Minted || len(got.CA.CertPEM) != 0 {
		t.Errorf("a list without ca-secrets produced material: %+v", got)
	}
}

// TestCAProvider pins the edge's provider derivation: an absent spec.ca is
// the cube provider, api's Default() having filled only what the user wrote.
func TestCAProvider(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		spec *v1alpha1.CASpec
		want string
	}{
		{name: "absent spec.ca is the cube provider", want: ca.ProviderCube},
		{name: "an explicit provider is used as written",
			spec: &v1alpha1.CASpec{Provider: v1alpha1.CAProviderCube}, want: "cube"},
		{name: "an unsupported provider reaches the switch unchanged",
			spec: &v1alpha1.CASpec{Provider: "user"}, want: "user"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := &v1alpha1.Config{Spec: v1alpha1.ConfigSpec{CA: tc.spec}}
			if got := caProvider(cfg); got != tc.want {
				t.Errorf("caProvider = %q, want %q", got, tc.want)
			}
		})
	}
}
