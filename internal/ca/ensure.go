package ca

import (
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
)

// EnsureRequest are the inputs to Ensure: the mint inputs plus whatever
// CA material the edge read from the cluster.
type EnsureRequest struct {
	MintRequest
	// ExistingCA is the CA material the edge read from the CA Secret, or
	// nil when the Secret is absent. Present-and-usable material is
	// reused byte-for-byte; present-and-unusable is CUBE-CA-002 — this
	// function never silently re-mints over material it cannot read.
	ExistingCA *Material
}

// EnsureResult is the effective CA material for this bootstrap.
type EnsureResult struct {
	// CA is the effective CA — the reused material byte-for-byte, or a
	// freshly minted one.
	CA Material
	// Leaf is the wildcard leaf, always freshly minted from the
	// effective CA (see docs/domains/ca.md: the leaf carries no trust
	// anchor and must track the current domain).
	Leaf Material
	// Minted reports whether the CA was minted rather than reused. It is
	// REPORTING ONLY — for bootstrap output. Nothing may be conditioned
	// on it: the ca.crt operator artifact syncs on every bootstrap
	// regardless, else CA reuse would strand a deleted file.
	Minted bool
}

// Ensure returns the effective CA material for a bootstrap: absent
// existing material mints a new CA, usable existing material is reused
// byte-for-byte (stable trust across re-bootstraps), and unusable
// existing material is CUBE-CA-002 with remediation naming the Secret to
// delete. The leaf is minted every time.
func Ensure(req EnsureRequest) (EnsureResult, error) {
	if req.ExistingCA == nil {
		caMaterial, leaf, err := Mint(req.MintRequest)
		if err != nil {
			return EnsureResult{}, err
		}
		return EnsureResult{CA: caMaterial, Leaf: leaf, Minted: true}, nil
	}
	// The usability gate is this function's own contract, so it is
	// checked here rather than left to MintLeaf's re-parse.
	if _, _, err := parseMaterial(*req.ExistingCA); err != nil {
		return EnsureResult{}, err
	}
	leaf, err := MintLeaf(*req.ExistingCA, req.MintRequest)
	if err != nil {
		return EnsureResult{}, err
	}
	return EnsureResult{CA: *req.ExistingCA, Leaf: leaf, Minted: false}, nil
}

// parseMaterial parses CA material and applies the usability criteria:
// both PEM blocks decode and parse, the key is an ECDSA key matching the
// certificate's public key, the certificate is a CA, and — when the
// certificate restricts key usage at all — that restriction includes
// certificate signing. Expiry and marker presence are deliberately NOT
// criteria — rotation is frozen, and a user-supplied CA in the Secret is
// the operator's choice.
func parseMaterial(m Material) (*x509.Certificate, *ecdsa.PrivateKey, error) {
	certBlock, _ := pem.Decode(m.CertPEM)
	if certBlock == nil {
		return nil, nil, newUnusableMaterialError("certificate PEM does not decode", nil)
	}
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return nil, nil, newUnusableMaterialError("certificate does not parse", err)
	}
	keyBlock, _ := pem.Decode(m.KeyPEM)
	if keyBlock == nil {
		return nil, nil, newUnusableMaterialError("private key PEM does not decode", nil)
	}
	parsed, err := x509.ParsePKCS8PrivateKey(keyBlock.Bytes)
	if err != nil {
		return nil, nil, newUnusableMaterialError("private key does not parse", err)
	}
	key, ok := parsed.(*ecdsa.PrivateKey)
	if !ok {
		return nil, nil, newUnusableMaterialError(fmt.Sprintf("private key is %T, not an ECDSA key", parsed), nil)
	}
	if !key.PublicKey.Equal(cert.PublicKey) {
		return nil, nil, newUnusableMaterialError("private key does not match the certificate", nil)
	}
	if !cert.IsCA {
		return nil, nil, newUnusableMaterialError("certificate is not a CA", nil)
	}
	// Zero KeyUsage is left unrestricted (RFC 5280: absent keyUsage places
	// no restriction on the certificate's use — mirrors Go's own x509
	// isValid check), but a set KeyUsage that excludes CertSign would let
	// this material be reused and then fail leaf verification with
	// x509.ConstraintViolationError instead of being caught here.
	if cert.KeyUsage != 0 && cert.KeyUsage&x509.KeyUsageCertSign == 0 {
		return nil, nil, newUnusableMaterialError("certificate cannot sign certificates", nil)
	}
	return cert, key, nil
}
