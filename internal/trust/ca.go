// Package trust implements cube-idp's D6 trust posture: a local CA (the
// mkcert mechanism), leaf certs for the gateway, canonical-hostname wiring,
// and — ONLY via the explicit `cube-idp trust` command — OS trust-store
// installation, fully reverted by `cube-idp down`. Nothing in this package
// touches the OS trust store implicitly.
package trust

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/cube-idp/cube-idp/internal/diag"
)

func Dir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", diag.Wrap(err, diag.CodeTrustCAFail, "cannot locate the user config directory", "set $HOME (or %AppData% on Windows)")
	}
	dir := filepath.Join(base, "cube-idp")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", diag.Wrap(err, diag.CodeTrustCAFail, "cannot create "+dir, "check permissions on your config directory")
	}
	return dir, nil
}

type CA struct {
	CertPath, KeyPath string
	Cert              *x509.Certificate
	Key               crypto.Signer
}

// EnsureCA loads the cube-idp local CA from dir, or creates one if none
// exists. Creation prefers adopting an existing mkcert root over
// generating a new one, so browsers that already trust mkcert show green
// locks with zero prompts. It is strictly idempotent: once a CA is present
// in dir (generated or adopted), subsequent calls always return that same
// CA — never regenerating and never re-checking mkcert.
func EnsureCA(dir string) (*CA, error) {
	certPath := filepath.Join(dir, "ca.crt")
	keyPath := filepath.Join(dir, "ca.key")
	if _, err := os.Stat(certPath); err == nil {
		return loadCA(certPath, keyPath)
	}
	if adoptMkcertCA(dir) {
		return loadCA(certPath, keyPath)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, diag.Wrap(err, diag.CodeTrustCAFail, "cannot generate the CA key", "retry; check system entropy")
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, diag.Wrap(err, diag.CodeTrustCAFail, "cannot generate a CA serial", "retry")
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{Organization: []string{"cube-idp local CA"}, CommonName: "cube-idp local CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		MaxPathLenZero:        true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, key.Public(), key)
	if err != nil {
		return nil, diag.Wrap(err, diag.CodeTrustCAFail, "cannot self-sign the CA", "retry")
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, diag.Wrap(err, diag.CodeTrustCAFail, "cannot serialize the CA key", "retry")
	}
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644); err != nil {
		return nil, diag.Wrap(err, diag.CodeTrustCAFail, "cannot write "+certPath, "check permissions")
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		return nil, diag.Wrap(err, diag.CodeTrustCAFail, "cannot write "+keyPath, "check permissions")
	}
	if err := os.Chmod(keyPath, 0o600); err != nil { // WriteFile does not chmod pre-existing files
		return nil, diag.Wrap(err, diag.CodeTrustCAFail, "cannot restrict permissions on "+keyPath, "check permissions")
	}
	return loadCA(certPath, keyPath)
}

// mkcertCAROOT returns mkcert's CA directory: $CAROOT if set (mkcert's own
// override), else the per-OS default mkcert uses.
func mkcertCAROOT() string {
	if v := os.Getenv("CAROOT"); v != "" {
		return v
	}
	switch runtime.GOOS {
	case "darwin":
		home, _ := os.UserHomeDir()
		return filepath.Join(home, "Library", "Application Support", "mkcert")
	case "windows":
		return filepath.Join(os.Getenv("LocalAppData"), "mkcert")
	default:
		if v := os.Getenv("XDG_DATA_HOME"); v != "" {
			return filepath.Join(v, "mkcert")
		}
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".local", "share", "mkcert")
	}
}

// adoptMkcertCA copies an existing mkcert root (cert+key) into dir so cube-idp
// issues leaves the user's browsers already trust. Presence-based: we do
// NOT verify OS-store trust (no portable query API) — if the mkcert root turns
// out untrusted, `cube-idp trust` installs it exactly like a generated CA.
//
// The source is fully validated BEFORE anything is copied: a broken mkcert
// install (unparseable cert or key, cert not a CA) is silently skipped so
// EnsureCA falls through to generating its own CA instead of copying garbage
// that would wedge every subsequent loadCA.
func adoptMkcertCA(dir string) (adopted bool) {
	caroot := mkcertCAROOT()
	cert, err1 := os.ReadFile(filepath.Join(caroot, "rootCA.pem"))
	key, err2 := os.ReadFile(filepath.Join(caroot, "rootCA-key.pem"))
	if err1 != nil || err2 != nil {
		return false
	}
	certBlock, _ := pem.Decode(cert)
	keyBlock, _ := pem.Decode(key)
	if certBlock == nil || keyBlock == nil {
		return false
	}
	parsed, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil || !parsed.IsCA {
		return false
	}
	if _, err := parseCAKey(keyBlock.Bytes); err != nil {
		return false
	}
	// Key first: an orphaned ca.key without ca.crt is harmless (the next
	// run's Stat(ca.crt) misses and re-adopts/regenerates cleanly), while an
	// orphaned ca.crt would wedge loadCA.
	keyPath := filepath.Join(dir, "ca.key")
	if os.WriteFile(keyPath, key, 0o600) != nil {
		return false
	}
	if os.Chmod(keyPath, 0o600) != nil { // WriteFile does not chmod pre-existing files
		return false
	}
	if os.WriteFile(filepath.Join(dir, "ca.crt"), cert, 0o644) != nil {
		return false
	}
	return true
}

func loadCA(certPath, keyPath string) (*CA, error) {
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return nil, diag.Wrap(err, diag.CodeTrustCAFail, "cannot read "+certPath, "delete the cube-idp config dir to regenerate the CA (browsers will need re-trusting)")
	}
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, diag.Wrap(err, diag.CodeTrustCAFail, "cannot read "+keyPath, "delete the cube-idp config dir to regenerate the CA (browsers will need re-trusting)")
	}
	certBlock, _ := pem.Decode(certPEM)
	keyBlock, _ := pem.Decode(keyPEM)
	if certBlock == nil || keyBlock == nil {
		return nil, diag.New(diag.CodeTrustCAFail, "CA files are not valid PEM", "delete the cube-idp config dir to regenerate the CA")
	}
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return nil, diag.Wrap(err, diag.CodeTrustCAFail, "CA certificate is corrupt", "delete the cube-idp config dir to regenerate the CA")
	}
	key, err := parseCAKey(keyBlock.Bytes)
	if err != nil {
		return nil, diag.Wrap(err, diag.CodeTrustCAFail, "CA key is corrupt", "delete the cube-idp config dir to regenerate the CA")
	}
	return &CA{CertPath: certPath, KeyPath: keyPath, Cert: cert, Key: key}, nil
}

// parseCAKey parses a CA private key, trying in order: SEC1 EC (what cube-idp
// itself generates), PKCS#8 (what mkcert's RSA roots use), and PKCS#1 RSA
// (older tooling). The result is asserted to crypto.Signer so IssueServerCert
// can sign leaves regardless of which format or key algorithm produced the CA.
func parseCAKey(der []byte) (crypto.Signer, error) {
	if key, err := x509.ParseECPrivateKey(der); err == nil {
		return key, nil
	}
	if key, err := x509.ParsePKCS8PrivateKey(der); err == nil {
		if signer, ok := key.(crypto.Signer); ok {
			return signer, nil
		}
		return nil, errors.New("PKCS#8 key does not implement crypto.Signer")
	}
	if key, err := x509.ParsePKCS1PrivateKey(der); err == nil {
		return key, nil
	}
	return nil, errors.New("key is not a recognized EC (SEC1), PKCS#8, or PKCS#1 RSA private key")
}

func (ca *CA) IssueServerCert(hosts []string, validity time.Duration) ([]byte, []byte, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, diag.Wrap(err, diag.CodeTrustCertIssueFail, "cannot generate a server key", "retry")
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, diag.Wrap(err, diag.CodeTrustCertIssueFail, "cannot generate a serial", "retry")
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{Organization: []string{"cube-idp"}, CommonName: hosts[0]},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(validity),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     hosts,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.Cert, key.Public(), ca.Key)
	if err != nil {
		return nil, nil, diag.Wrap(err, diag.CodeTrustCertIssueFail, "cannot sign the server certificate", "retry; if it persists, regenerate the CA")
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, nil, diag.Wrap(err, diag.CodeTrustCertIssueFail, "cannot serialize the server key", "retry")
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	return certPEM, keyPEM, nil
}

// LeafStillValid reports whether certPEM was signed by this CA, covers every
// host, and has at least margin left before expiry. `up` uses it to avoid
// re-issuing (and thus avoid perpetual diffs) on every run.
func (ca *CA) LeafStillValid(certPEM []byte, hosts []string, margin time.Duration) bool {
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return false
	}
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return false
	}
	if time.Now().Add(margin).After(leaf.NotAfter) {
		return false
	}
	pool := x509.NewCertPool()
	pool.AddCert(ca.Cert)
	for _, h := range hosts {
		probe := h
		if len(h) > 1 && h[0] == '*' { // VerifyHostname rejects literal wildcards; probe a concrete name
			probe = "probe" + h[1:]
		}
		if _, err := leaf.Verify(x509.VerifyOptions{DNSName: probe, Roots: pool,
			KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}); err != nil {
			return false
		}
	}
	return true
}
