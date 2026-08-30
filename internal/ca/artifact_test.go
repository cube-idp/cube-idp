package ca_test

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cube-idp/cube-idp/internal/ca"
)

// TestFingerprint checks the digest against one computed in-test from
// the certificate's own DER, so the assertion does not simply repeat the
// implementation's choice of input.
func TestFingerprint(t *testing.T) {
	caMaterial, _ := mustMint(t, testCube, testDomain, testNow(), 1)
	sum := sha256.Sum256(parseCert(t, caMaterial.CertPEM).Raw)
	want := hex.EncodeToString(sum[:])

	got, err := ca.Fingerprint(caMaterial.CertPEM)
	if err != nil {
		t.Fatalf("Fingerprint() error = %v", err)
	}
	if got != want {
		t.Errorf("Fingerprint() = %s, want %s", got, want)
	}
	// Asserted independently of want, which is lowercase by construction:
	// phase 2 compares this value case-insensitively against the uppercase
	// digest OS trust tooling prints, so the rendering is contract.
	if len(got) != 64 || strings.ContainsAny(got, ":ABCDEF") {
		t.Errorf("Fingerprint() = %q, want 64 lowercase colon-free hex characters", got)
	}
}

// TestFingerprintRejects: anything that is not a certificate is an
// error, never an empty string a caller could record in the ledger.
func TestFingerprintRejects(t *testing.T) {
	caMaterial, _ := mustMint(t, testCube, testDomain, testNow(), 1)
	cases := []struct {
		name string
		pem  []byte
	}{
		{"empty input", nil},
		{"not PEM at all", []byte("fce7a0ea053961041be2f12aa126ab20")},
		{"a PEM block that is not a certificate", caMaterial.KeyPEM},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ca.Fingerprint(tc.pem)
			if err == nil {
				t.Fatalf("Fingerprint() = %q, want an error", got)
			}
			if got != "" {
				t.Errorf("Fingerprint() = %q on error, want the empty string", got)
			}
		})
	}
}

// TestArtifactPaths pins the operator-artifact layout: the names are
// user-visible and frozen once a machine has files on disk.
func TestArtifactPaths(t *testing.T) {
	if ca.DirName != ".cube-idp" {
		t.Errorf("DirName = %q, want %q", ca.DirName, ".cube-idp")
	}
	root := filepath.Join("/home", "u", ca.DirName)
	if got, want := ca.LedgerPath(root), filepath.Join(root, "trust.yaml"); got != want {
		t.Errorf("LedgerPath() = %q, want %q", got, want)
	}
	if got, want := ca.CertPath(root, "dev"), filepath.Join(root, "dev", "ca.crt"); got != want {
		t.Errorf("CertPath() = %q, want %q", got, want)
	}
}
