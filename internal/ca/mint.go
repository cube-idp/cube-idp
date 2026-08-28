package ca

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"time"
)

const (
	// caValidityYears and leafValidityYears are the v0 validity windows —
	// long-lived by design, because M11 builds no rotation machinery.
	caValidityYears   = 10
	leafValidityYears = 2

	// clockSkew backdates NotBefore so a certificate minted seconds ago is
	// never "not yet valid" on a host whose clock lags.
	clockSkew = time.Hour

	// serialBits sizes the serial number: RFC 5280 requires a positive
	// integer of at most 20 octets.
	serialBits = 128
)

// MintRequest are the inputs to a mint: everything is injected, so the
// result is a function of its arguments and nothing else.
type MintRequest struct {
	// CubeName is metadata.name of the cube; it forms the CA's marker CN.
	CubeName string
	// Domain is the cube's base domain (spec.gateway.domain). The leaf
	// covers the wildcard *.<Domain>.
	Domain string
	// Now anchors both certificates' validity window. Injected so tests
	// are deterministic and the domain never reads the clock.
	Now time.Time
	// Rand is the entropy source for keys, serial numbers, and
	// signatures. Injected; the edge passes crypto/rand.Reader.
	Rand io.Reader
}

// Mint mints a fresh self-signed CA and a wildcard leaf signed by it.
// Both certificates are anchored at req.Now (CA ~10 years, leaf ~2
// years — long-lived by design; M11 builds no rotation machinery).
func Mint(req MintRequest) (caMaterial, leafMaterial Material, err error) {
	caMaterial, err = mintCA(req)
	if err != nil {
		return Material{}, Material{}, err
	}
	leafMaterial, err = MintLeaf(caMaterial, req)
	if err != nil {
		return Material{}, Material{}, err
	}
	return caMaterial, leafMaterial, nil
}

// MintLeaf mints a wildcard leaf signed by existing CA material — the
// reuse path, where the CA is kept byte-for-byte and only the leaf is
// minted.
func MintLeaf(ca Material, req MintRequest) (Material, error) {
	caCert, caKey, err := parseMaterial(ca)
	if err != nil {
		return Material{}, err
	}
	key, serial, err := newKeyAndSerial(req.Rand)
	if err != nil {
		return Material{}, newMintError(subjectLeaf, err)
	}
	m, err := signAndEncode(req.Rand, leafTemplate(req.Domain, req.Now, serial), caCert, key, caKey)
	if err != nil {
		return Material{}, newMintError(subjectLeaf, err)
	}
	return m, nil
}

// mintCA mints the self-signed CA: its own template is its own parent
// and its own key is the signer.
func mintCA(req MintRequest) (Material, error) {
	key, serial, err := newKeyAndSerial(req.Rand)
	if err != nil {
		return Material{}, newMintError(subjectCA, err)
	}
	tpl := caTemplate(req.CubeName, req.Now, serial)
	m, err := signAndEncode(req.Rand, tpl, tpl, key, key)
	if err != nil {
		return Material{}, newMintError(subjectCA, err)
	}
	return m, nil
}

// newKeyAndSerial draws the two random inputs every certificate needs.
func newKeyAndSerial(rnd io.Reader) (*ecdsa.PrivateKey, *big.Int, error) {
	key, err := newKey(rnd)
	if err != nil {
		return nil, nil, err
	}
	serial, err := newSerial(rnd)
	if err != nil {
		return nil, nil, err
	}
	return key, serial, nil
}

// newKey generates a P-256 key from the injected entropy source.
func newKey(rnd io.Reader) (*ecdsa.PrivateKey, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rnd)
	if err != nil {
		return nil, fmt.Errorf("generate P-256 key: %w", err)
	}
	return key, nil
}

// newSerial draws a serial number from the injected entropy source.
func newSerial(rnd io.Reader) (*big.Int, error) {
	// rand.Int draws from [0,max), so draw from [0,2^128-1) and shift by
	// one: RFC 5280 requires the serial to be strictly positive.
	limit := new(big.Int).Lsh(big.NewInt(1), serialBits)
	n, err := rand.Int(rnd, limit.Sub(limit, big.NewInt(1)))
	if err != nil {
		return nil, fmt.Errorf("generate serial number: %w", err)
	}
	return n.Add(n, big.NewInt(1)), nil
}

// caTemplate builds the CA certificate template, carrying the marker
// identity and the constraints of an issuer that signs end-entity
// certificates only.
func caTemplate(cubeName string, now time.Time, serial *big.Int) *x509.Certificate {
	return &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:         CommonName(cubeName),
			OrganizationalUnit: []string{MarkerOrganizationalUnit},
		},
		NotBefore:             now.Add(-clockSkew),
		NotAfter:              now.AddDate(caValidityYears, 0, 0),
		SignatureAlgorithm:    x509.ECDSAWithSHA256,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            0,
		MaxPathLenZero:        true,
	}
}

// leafTemplate builds the gateway serving certificate's template. It
// covers the wildcard *.<domain> and nothing else: the apex does not
// resolve in-cluster, so covering it would be surface with no consumer.
func leafTemplate(domain string, now time.Time, serial *big.Int) *x509.Certificate {
	wildcard := "*." + domain
	return &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: wildcard},
		DNSNames:              []string{wildcard},
		NotBefore:             now.Add(-clockSkew),
		NotAfter:              now.AddDate(leafValidityYears, 0, 0),
		SignatureAlgorithm:    x509.ECDSAWithSHA256,
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
}

// signAndEncode signs tpl under parent with signer and PEM-encodes the
// result beside key — the one place Material is built. key is the
// subject's own key pair; signer is the issuer's (the same key for a
// self-signed CA).
func signAndEncode(rnd io.Reader, tpl, parent *x509.Certificate, key, signer *ecdsa.PrivateKey) (Material, error) {
	der, err := x509.CreateCertificate(rnd, tpl, parent, &key.PublicKey, signer)
	if err != nil {
		return Material{}, fmt.Errorf("create certificate: %w", err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return Material{}, fmt.Errorf("marshal private key: %w", err)
	}
	return Material{
		CertPEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		KeyPEM:  pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}),
	}, nil
}
