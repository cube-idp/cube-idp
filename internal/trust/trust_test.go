package trust

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cube-idp/cube-idp/internal/diag"
)

func TestEnsureCAIsIdempotent(t *testing.T) {
	// Isolate from any real mkcert install on the developer's machine: an
	// empty CAROOT means adoptMkcertCA finds nothing, so EnsureCA generates.
	t.Setenv("CAROOT", t.TempDir())

	dir := t.TempDir()
	ca1, err := EnsureCA(dir)
	if err != nil {
		t.Fatal(err)
	}
	ca2, err := EnsureCA(dir)
	if err != nil {
		t.Fatal(err)
	}
	if ca1.Cert.SerialNumber.Cmp(ca2.Cert.SerialNumber) != 0 {
		t.Fatal("EnsureCA regenerated an existing CA — OS trust would silently break")
	}
	info, err := os.Stat(ca1.KeyPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("CA key must be 0600, got %v", info.Mode().Perm())
	}
	if !ca1.Cert.IsCA {
		t.Fatal("CA cert missing IsCA")
	}
}

func TestIssueServerCertVerifiesAgainstCA(t *testing.T) {
	t.Setenv("CAROOT", t.TempDir())

	ca, err := EnsureCA(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	hosts := []string{"cube-idp.localtest.me", "*.cube-idp.localtest.me"}
	certPEM, keyPEM, err := ca.IssueServerCert(hosts, 365*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(keyPEM) == 0 {
		t.Fatal("no key produced")
	}
	block, _ := pem.Decode(certPEM)
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(ca.Cert)
	if _, err := leaf.Verify(x509.VerifyOptions{DNSName: "gitea.cube-idp.localtest.me", Roots: pool,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}); err != nil {
		t.Fatalf("leaf does not verify for a wildcard subdomain: %v", err)
	}
	if !ca.LeafStillValid(certPEM, hosts, 30*24*time.Hour) {
		t.Fatal("fresh leaf must count as valid")
	}
	if ca.LeafStillValid(certPEM, []string{"other.example.com"}, 30*24*time.Hour) {
		t.Fatal("leaf must not count as valid for hosts it does not cover")
	}
}

func TestStateRoundTripAndCorrupt(t *testing.T) {
	t.Setenv("CAROOT", t.TempDir())

	dir := t.TempDir()
	s, err := LoadState(dir)
	if err != nil || s.Installed {
		t.Fatalf("missing state must load as zero value: %+v %v", s, err)
	}
	if err := SaveState(dir, &State{Installed: true, CACert: "/x/ca.crt"}); err != nil {
		t.Fatal(err)
	}
	s, err = LoadState(dir)
	if err != nil || !s.Installed || s.CACert != "/x/ca.crt" {
		t.Fatalf("round trip: %+v %v", s, err)
	}
	os.WriteFile(filepath.Join(dir, "trust-state.yaml"), []byte("{{{"), 0o644)
	_, err = LoadState(dir)
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != diag.CodeTrustStateFail {
		t.Fatalf("want %s, got %v", diag.CodeTrustStateFail, err)
	}
}

func TestEnsureCAAdoptsMkcertRoot(t *testing.T) {
	// Build a fake mkcert CAROOT: generate a CA the normal way, then lay its
	// files out under mkcert's names.
	seed := t.TempDir()
	t.Setenv("CAROOT", t.TempDir()) // isolate: force a fresh EC CA for the seed, not a real mkcert install
	mk, err := EnsureCA(seed)
	if err != nil {
		t.Fatal(err)
	}
	caroot := t.TempDir()
	for src, dst := range map[string]string{
		mk.CertPath: filepath.Join(caroot, "rootCA.pem"),
		mk.KeyPath:  filepath.Join(caroot, "rootCA-key.pem"),
	} {
		raw, err := os.ReadFile(src)
		if err != nil {
			t.Fatal(err)
		}
		os.WriteFile(dst, raw, 0o600)
	}
	t.Setenv("CAROOT", caroot) // mkcert's own override env var

	dir := t.TempDir()
	ca, err := EnsureCA(dir)
	if err != nil {
		t.Fatal(err)
	}
	if ca.Cert.SerialNumber.Cmp(mk.Cert.SerialNumber) != 0 {
		t.Fatal("EnsureCA must adopt the mkcert root when one exists (D12)")
	}
	// adoption is a copy: the cube-idp dir is self-contained afterwards
	if _, err := os.Stat(filepath.Join(dir, "ca.crt")); err != nil {
		t.Fatal("adopted CA must be copied into the cube-idp dir")
	}
	// the adopted key must always end up 0600 (WriteFile alone does not
	// chmod pre-existing files)
	info, err := os.Stat(ca.KeyPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("adopted CA key must be 0600, got %v", info.Mode().Perm())
	}
	// and a cube-idp CA, once present, wins over CAROOT (no flip-flop)
	again, err := EnsureCA(dir)
	if err != nil || again.Cert.SerialNumber.Cmp(ca.Cert.SerialNumber) != 0 {
		t.Fatalf("EnsureCA must stay stable once adopted: %v", err)
	}
}

// TestEnsureCASkipsGarbageMkcertRoot: a broken mkcert install (unparseable
// cert or key) must not brick cube-idp — adoption is skipped entirely (no
// files copied) and EnsureCA falls through to generating a fresh CA.
func TestEnsureCASkipsGarbageMkcertRoot(t *testing.T) {
	caroot := t.TempDir()
	if err := os.WriteFile(filepath.Join(caroot, "rootCA.pem"), []byte("not a cert"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(caroot, "rootCA-key.pem"), []byte("not a key"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CAROOT", caroot)

	dir := t.TempDir()
	ca, err := EnsureCA(dir)
	if err != nil {
		t.Fatalf("EnsureCA must generate a fresh CA when the mkcert root is garbage, got: %v", err)
	}
	if !ca.Cert.IsCA {
		t.Fatal("generated cert missing IsCA")
	}
	if ca.Cert.Subject.CommonName != "cube-idp local CA" {
		t.Fatalf("expected a generated cube-idp CA, got CN=%q (garbage adopted?)", ca.Cert.Subject.CommonName)
	}
}

// TestEnsureCAAdoptsMkcertRootRSA covers the actual mkcert reality: mkcert's
// root key is RSA in PKCS#8 form, not the EC/SEC1 shape cube-idp generates
// itself. This exercises loadCA's ParsePKCS8PrivateKey fallback (the EC-seed
// adoption test above only ever exercises the ParseECPrivateKey branch) and
// confirms IssueServerCert can sign leaves off an RSA CA key.
func TestEnsureCAAdoptsMkcertRootRSA(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{Organization: []string{"mkcert development CA"}, CommonName: "fake mkcert root CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	caroot := t.TempDir()
	if err := os.WriteFile(filepath.Join(caroot, "rootCA.pem"),
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(caroot, "rootCA-key.pem"),
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CAROOT", caroot)

	dir := t.TempDir()
	ca, err := EnsureCA(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ca.Key.(*rsa.PrivateKey); !ok {
		t.Fatalf("adopted RSA mkcert key must parse as *rsa.PrivateKey via the PKCS#8 fallback, got %T", ca.Key)
	}

	certPEM, keyPEM, err := ca.IssueServerCert([]string{"example.cube-idp.localtest.me"}, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(keyPEM) == 0 {
		t.Fatal("no key produced")
	}
	block, _ := pem.Decode(certPEM)
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(ca.Cert)
	if _, err := leaf.Verify(x509.VerifyOptions{DNSName: "example.cube-idp.localtest.me", Roots: pool,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}); err != nil {
		t.Fatalf("leaf signed by adopted RSA CA does not verify: %v", err)
	}
}
