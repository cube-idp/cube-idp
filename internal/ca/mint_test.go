package ca_test

import (
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"io"
	mrand "math/rand/v2"
	"reflect"
	"testing"
	"time"

	"github.com/cube-idp/cube-idp/internal/ca"
	"github.com/cube-idp/cube-idp/internal/cubeerr"
)

const (
	testCube   = "demo"
	testDomain = "demo.test"
)

// TestMint pins every structural property of a mint. Byte-exact goldens
// are impossible here — crypto/internal/randutil consumes a byte from
// the caller's reader with 50% probability and ECDSA signatures are
// variable-length DER — so structure is what a mint can assert.
func TestMint(t *testing.T) {
	now := testNow()
	caMaterial, leafMaterial := mustMint(t, testCube, testDomain, now, 1)
	caCert := parseCert(t, caMaterial.CertPEM)
	leafCert := parseCert(t, leafMaterial.CertPEM)

	cases := []struct {
		name string
		got  any
		want any
	}{
		{"CA common name", caCert.Subject.CommonName, "cube-idp demo CA"},
		{"CA carries the marker", ca.HasMarker(caCert, testCube), true},
		{"CA organizational unit", caCert.Subject.OrganizationalUnit, []string{"cube-idp.dev"}},
		{"CA is a CA", caCert.IsCA, true},
		{"CA basic constraints are valid", caCert.BasicConstraintsValid, true},
		{"CA path length is zero", caCert.MaxPathLenZero, true},
		{"CA key usage signs certs and CRLs", caCert.KeyUsage, x509.KeyUsageCertSign | x509.KeyUsageCRLSign},
		{"CA signature algorithm", caCert.SignatureAlgorithm, x509.ECDSAWithSHA256},
		{"CA not before trails now by an hour", stamp(caCert.NotBefore), stamp(now.Add(-time.Hour))},
		{"CA not after is ten years out", stamp(caCert.NotAfter), stamp(now.AddDate(10, 0, 0))},
		{"CA serial is positive", caCert.SerialNumber.Sign(), 1},
		{"CA serial fits twenty octets", len(caCert.SerialNumber.Bytes()) <= 20, true},
		{"CA key is P-256", keyCurveName(t, caMaterial.KeyPEM), "P-256"},
		{"leaf common name is the wildcard", leafCert.Subject.CommonName, "*.demo.test"},
		{"leaf covers the wildcard and nothing else", leafCert.DNSNames, []string{"*.demo.test"}},
		{"leaf has no IP SANs", len(leafCert.IPAddresses), 0},
		{"leaf is not a CA", leafCert.IsCA, false},
		{"leaf key usage is digital signature", leafCert.KeyUsage, x509.KeyUsageDigitalSignature},
		{"leaf extended key usage is server auth", leafCert.ExtKeyUsage, []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}},
		{"leaf issuer is the CA", leafCert.Issuer.CommonName, caCert.Subject.CommonName},
		{"leaf not before trails now by an hour", stamp(leafCert.NotBefore), stamp(now.Add(-time.Hour))},
		{"leaf not after is two years out", stamp(leafCert.NotAfter), stamp(now.AddDate(2, 0, 0))},
		{"leaf serial is positive", leafCert.SerialNumber.Sign(), 1},
		{"leaf key is P-256", keyCurveName(t, leafMaterial.KeyPEM), "P-256"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !reflect.DeepEqual(tc.got, tc.want) {
				t.Errorf("got %v, want %v", tc.got, tc.want)
			}
		})
	}
}

// TestMintChainVerifies checks the chain the gateway actually presents.
// Every VerifyOptions carries CurrentTime: the certificates are minted at
// an injected fixed Now, so without it the negative rows would fail on
// the validity window and pass for the wrong reason.
func TestMintChainVerifies(t *testing.T) {
	now := testNow()
	caMaterial, leafMaterial := mustMint(t, testCube, testDomain, now, 1)
	leaf := parseCert(t, leafMaterial.CertPEM)
	pool := certPool(t, caMaterial.CertPEM)

	if _, err := leaf.Verify(x509.VerifyOptions{
		Roots: pool, DNSName: "app." + testDomain, CurrentTime: now,
	}); err != nil {
		t.Fatalf("verify app.%s against the minting CA: %v", testDomain, err)
	}

	// The apex is deliberately uncovered: CoreDNS rewrites require at
	// least one label, so nothing resolves it in-cluster. The error type
	// is asserted so this cannot pass on an expired validity window.
	_, err := leaf.Verify(x509.VerifyOptions{Roots: pool, DNSName: testDomain, CurrentTime: now})
	var hostErr x509.HostnameError
	if !errors.As(err, &hostErr) {
		t.Errorf("verify of the bare apex %s = %v, want x509.HostnameError", testDomain, err)
	}

	otherCA, _ := mustMint(t, "other", testDomain, now, 2)
	_, err = leaf.Verify(x509.VerifyOptions{
		Roots: certPool(t, otherCA.CertPEM), DNSName: "app." + testDomain, CurrentTime: now,
	})
	var authErr x509.UnknownAuthorityError
	if !errors.As(err, &authErr) {
		t.Errorf("verify against an unrelated CA = %v, want x509.UnknownAuthorityError", err)
	}
}

// TestMintDistinct pins the property that survives randutil's
// probabilistic byte read: two mints are never the same material.
func TestMintDistinct(t *testing.T) {
	now := testNow()
	firstCA, firstLeaf := mustMint(t, testCube, testDomain, now, 1)
	secondCA, secondLeaf := mustMint(t, testCube, testDomain, now, 2)

	if s1, s2 := parseCert(t, firstCA.CertPEM).SerialNumber, parseCert(t, secondCA.CertPEM).SerialNumber; s1.Cmp(s2) == 0 {
		t.Errorf("two CA mints share serial %s", s1)
	}
	if string(firstCA.KeyPEM) == string(secondCA.KeyPEM) {
		t.Errorf("two CA mints share a private key")
	}
	if string(firstLeaf.KeyPEM) == string(secondLeaf.KeyPEM) {
		t.Errorf("two leaf mints share a private key")
	}
}

// TestMintEntropyFailure covers the one mint failure a caller can
// provoke: an entropy source that cannot be read.
func TestMintEntropyFailure(t *testing.T) {
	_, _, err := ca.Mint(ca.MintRequest{
		CubeName: testCube, Domain: testDomain, Now: testNow(), Rand: failingReader{},
	})
	_ = assertCode(t, err, ca.CodeMint)
}

// failingReader is an entropy source that always fails.
type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("no entropy") }

// testNow is the fixed instant every test mints at.
func testNow() time.Time { return time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC) }

// stamp renders a certificate time for comparison, sidestepping
// time.Time's unexported location and monotonic fields.
func stamp(t time.Time) string { return t.UTC().Format(time.RFC3339) }

// seededReader is a deterministic entropy source. It is deliberately not
// all zeros: FIPS ECDSA key generation rejection-samples and would spin.
func seededReader(seed byte) io.Reader {
	var key [32]byte
	for i := range key {
		key[i] = seed + byte(i)
	}
	return mrand.NewChaCha8(key)
}

// mustMint mints from a seeded reader or fails the test.
func mustMint(t *testing.T, cube, domain string, now time.Time, seed byte) (caMaterial, leaf ca.Material) {
	t.Helper()
	caMaterial, leaf, err := ca.Mint(ca.MintRequest{
		CubeName: cube, Domain: domain, Now: now, Rand: seededReader(seed),
	})
	if err != nil {
		t.Fatalf("Mint(%s) error = %v", cube, err)
	}
	return caMaterial, leaf
}

// parseCert decodes and parses one PEM certificate.
func parseCert(t *testing.T, certPEM []byte) *x509.Certificate {
	t.Helper()
	block, _ := pem.Decode(certPEM)
	if block == nil {
		t.Fatalf("certificate PEM does not decode: %q", certPEM)
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("ParseCertificate() error = %v", err)
	}
	return cert
}

// keyCurveName parses a PKCS#8 PEM key and names its curve.
func keyCurveName(t *testing.T, keyPEM []byte) string {
	t.Helper()
	block, _ := pem.Decode(keyPEM)
	if block == nil {
		t.Fatalf("private key PEM does not decode")
	}
	if block.Type != "PRIVATE KEY" {
		t.Fatalf("PEM block type = %q, want PRIVATE KEY (PKCS#8)", block.Type)
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		t.Fatalf("ParsePKCS8PrivateKey() error = %v", err)
	}
	key, ok := parsed.(*ecdsa.PrivateKey)
	if !ok {
		t.Fatalf("private key is %T, want *ecdsa.PrivateKey", parsed)
	}
	return key.Curve.Params().Name
}

// certPool builds a root pool holding one PEM certificate.
func certPool(t *testing.T, certPEM []byte) *x509.CertPool {
	t.Helper()
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(certPEM) {
		t.Fatalf("certificate PEM was not accepted into a pool")
	}
	return pool
}

// assertCode asserts error identity by code, never by message, and
// returns the coded error so callers can inspect its remediation.
func assertCode(t *testing.T, err error, want cubeerr.Code) *cubeerr.Coded {
	t.Helper()
	var coded *cubeerr.Coded
	if !errors.As(err, &coded) {
		t.Fatalf("error = %v, want *cubeerr.Coded", err)
	}
	if coded.Code != want {
		t.Fatalf("code = %s, want %s", coded.Code, want)
	}
	return coded
}
