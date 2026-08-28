// Package ca owns the cube's certificate authority: minting the per-cube
// CA and its wildcard gateway leaf, the reuse contract that keeps trust
// stable across re-bootstraps, and the marker identity every minted CA
// carries. It is PURE — minting is a function of injected inputs
// (existing key material, injected entropy, injected now), Secret
// contents are returned as objects, and every I/O (reading the existing
// Secret, applying Secrets, writing operator artifacts) stays at the CLI
// edge. Contract: docs/domains/ca.md.
package ca

import (
	"crypto/x509"
	"fmt"
	"slices"
)

// ProviderCube is the cube-owned, stdlib-minted CA provider — the
// default and the only value M11 admits. The provider seam is
// deliberately not a Go interface yet (second-implementation doctrine);
// the edge switches on this string exactly as the engine factory does.
const ProviderCube = "cube"

// MarkerOrganizationalUnit is the OU every cube-idp-minted CA carries.
// Together with the CommonName it is the marker identity: enough to
// recognize a cube-idp CA wherever it is encountered, so identification
// never needs orphan-scan machinery.
const MarkerOrganizationalUnit = "cube-idp.dev"

// Material is a PEM-encoded certificate and its private key — the
// domain's only key-material vocabulary. Bytes in, bytes out: parsing
// happens here, storage and transport belong to the caller.
type Material struct {
	// CertPEM is the PEM-encoded X.509 certificate.
	CertPEM []byte
	// KeyPEM is the PEM-encoded PKCS#8 private key.
	KeyPEM []byte
}

// CommonName returns the marker CN a cube's CA carries:
// "cube-idp <cube-name> CA".
func CommonName(cubeName string) string {
	return fmt.Sprintf("cube-idp %s CA", cubeName)
}

// HasMarker reports whether a parsed certificate carries this cube's
// full marker identity — both the CN and the OU. The marker alone never
// authorizes a trust-store removal (the ledger fingerprint is the
// identity); this reports the marker half of that check.
func HasMarker(cert *x509.Certificate, cubeName string) bool {
	if cert == nil || cert.Subject.CommonName != CommonName(cubeName) {
		return false
	}
	return slices.Contains(cert.Subject.OrganizationalUnit, MarkerOrganizationalUnit)
}
