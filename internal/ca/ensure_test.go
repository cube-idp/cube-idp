package ca_test

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cube-idp/cube-idp/internal/ca"
	"github.com/cube-idp/cube-idp/internal/cubeerr"
)

// TestEnsure is the reuse contract: absence mints, usable material is
// kept byte-for-byte so trust survives a re-bootstrap, and unusable
// material is refused rather than silently re-minted over.
func TestEnsure(t *testing.T) {
	now := testNow()
	existing, _ := mustMint(t, testCube, testDomain, now, 1)
	other, otherLeaf := mustMint(t, testCube, testDomain, now, 2)

	cases := []struct {
		name       string
		existingCA *ca.Material
		wantMinted bool
		wantCode   cubeerr.Code
	}{
		{"absent material mints a new CA", nil, true, ""},
		{"usable material is reused", &existing, false, ""},
		{"unusable: certificate is not PEM",
			&ca.Material{CertPEM: []byte("not a pem block"), KeyPEM: existing.KeyPEM}, false, ca.CodeUnusableMaterial},
		{"unusable: key is empty",
			&ca.Material{CertPEM: existing.CertPEM}, false, ca.CodeUnusableMaterial},
		{"unusable: key does not match the certificate",
			&ca.Material{CertPEM: existing.CertPEM, KeyPEM: other.KeyPEM}, false, ca.CodeUnusableMaterial},
		{"unusable: certificate is not a CA", &otherLeaf, false, ca.CodeUnusableMaterial},
		{"unusable: CA cannot sign certificates", mintCannotSignCA(t, now), false, ca.CodeUnusableMaterial},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ca.Ensure(ca.EnsureRequest{
				MintRequest: mintRequest(testDomain, now, 3),
				ExistingCA:  tc.existingCA,
			})
			if tc.wantCode != "" {
				coded := assertCode(t, err, tc.wantCode)
				if !strings.Contains(coded.Remediation, "cube CA Secret") {
					t.Errorf("remediation %q must name the cube CA Secret to delete", coded.Remediation)
				}
				return
			}
			if err != nil {
				t.Fatalf("Ensure() error = %v", err)
			}
			if got.Minted != tc.wantMinted {
				t.Errorf("Minted = %v, want %v", got.Minted, tc.wantMinted)
			}
			if !ca.HasMarker(parseCert(t, got.CA.CertPEM), testCube) {
				t.Errorf("effective CA does not carry the cube's marker identity")
			}
			if tc.existingCA != nil {
				assertReused(t, got.CA, *tc.existingCA)
			}
			assertLeafChainsTo(t, got, now, testDomain)
		})
	}
}

// TestEnsureDomainChangeRemintsLeaf is the regression test for minting
// the leaf every bootstrap: spec.gateway.domain is user-editable, so a
// reused leaf could carry SANs that no longer cover the cube's domain.
func TestEnsureDomainChangeRemintsLeaf(t *testing.T) {
	now := testNow()
	existing, _ := mustMint(t, testCube, testDomain, now, 1)

	const newDomain = "renamed.test"
	got, err := ca.Ensure(ca.EnsureRequest{
		MintRequest: mintRequest(newDomain, now, 4),
		ExistingCA:  &existing,
	})
	if err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	assertReused(t, got.CA, existing)
	if names, want := parseCert(t, got.Leaf.CertPEM).DNSNames, []string{"*." + newDomain}; !slices.Equal(names, want) {
		t.Errorf("leaf DNS names = %v, want %v", names, want)
	}
	assertLeafChainsTo(t, got, now, newDomain)
}

// mintRequest builds this cube's mint inputs for a domain and seed.
func mintRequest(domain string, now time.Time, seed byte) ca.MintRequest {
	return ca.MintRequest{CubeName: testCube, Domain: domain, Now: now, Rand: seededReader(seed)}
}

// assertReused asserts CA material was carried over byte-for-byte.
func assertReused(t *testing.T, got, want ca.Material) {
	t.Helper()
	if !bytes.Equal(got.CertPEM, want.CertPEM) {
		t.Errorf("CA certificate was not reused byte-for-byte")
	}
	if !bytes.Equal(got.KeyPEM, want.KeyPEM) {
		t.Errorf("CA private key was not reused byte-for-byte")
	}
}

// mintCannotSignCA mints a self-signed, IsCA-true certificate whose
// KeyUsage is restricted to digital signature only — a certificate that
// declares itself a CA but is not permitted to sign other certificates.
// It regression-tests the KeyUsage criterion in parseMaterial: without
// it, this material would be reused and only fail later, when the leaf
// it signed fails x509 chain verification.
func mintCannotSignCA(t *testing.T, now time.Time) *ca.Material {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), seededReader(9))
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	tpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "cannot-sign CA"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.AddDate(10, 0, 0),
		SignatureAlgorithm:    x509.ECDSAWithSHA256,
		KeyUsage:              x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(seededReader(9), tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("CreateCertificate() error = %v", err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("MarshalPKCS8PrivateKey() error = %v", err)
	}
	return &ca.Material{
		CertPEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		KeyPEM:  pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}),
	}
}

// assertLeafChainsTo asserts the result's leaf verifies against the
// result's own CA for a name under the domain.
func assertLeafChainsTo(t *testing.T, r ca.EnsureResult, now time.Time, domain string) {
	t.Helper()
	_, err := parseCert(t, r.Leaf.CertPEM).Verify(x509.VerifyOptions{
		Roots: certPool(t, r.CA.CertPEM), DNSName: "app." + domain, CurrentTime: now,
	})
	if err != nil {
		t.Errorf("leaf does not verify against the effective CA: %v", err)
	}
}
