package ca

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"strings"
)

// Runner executes one of the operating system's trust tools and returns
// what it printed. stdout and stderr are separate because both
// security(1) and trust(1) put the actionable reason on stderr while
// stdout carries the data a driver parses.
//
// It is the drivers' only route to the operating system: os/exec lives
// at the CLI edge, so every store operation is exercised in the hermetic
// gate against a fake. A Runner that cannot find the tool must report it
// with an error satisfying errors.Is(err, exec.ErrNotFound) — os/exec's
// own sentinel, which the edge's Runner returns naturally.
type Runner func(ctx context.Context, name string, args ...string) (stdout, stderr []byte, err error)

// Store is one user-scope OS trust store. Two implementations exist —
// the macOS login keychain and the p11-kit user anchor store — so the
// seam is earned. Both are user scope by construction: system-scope
// stores are frozen for M11, and no implementation ever invokes sudo.
type Store interface {
	// Name is the value recorded in a ledger entry's store field.
	Name() string
	// Install adds the CA certificate at certPath as a user-scope trust
	// anchor. It is INTERACTIVE on macOS — add-trusted-cert raises a GUI
	// authorization prompt — so it is only ever reached from an explicit
	// `trust install`, never from bootstrap.
	Install(ctx context.Context, cube, certPath string) error
	// Remove verifies both the ledger fingerprint and the full marker
	// before deleting anything, and reports Found=false instead of an
	// error when the store no longer holds the certificate.
	Remove(ctx context.Context, req RemoveRequest) (RemoveOutcome, error)
}

// RemoveRequest is everything a store needs to remove a cube's CA. It
// carries the certificate twice over because the two stores differ:
// p11-kit's `trust anchor --remove` takes a FILE and so needs the path,
// while verifying the identity needs the bytes — and the domain performs
// no I/O, so the edge reads the artifact and passes both.
type RemoveRequest struct {
	// Cube is the cube whose CA is being removed; it supplies the marker
	// CN both halves of the identity check are made against.
	Cube string
	// Fingerprint is the ledger's identity for that CA. The certificate
	// is located and verified by this value: the marker alone never
	// authorizes a removal, because every cube's CA shares its shape.
	Fingerprint string
	// CertPath is the emitted artifact's path, passed to tools that take
	// a file rather than a digest.
	CertPath string
	// CertPEM is that file's bytes, or nil when the file is absent. A
	// store that can only verify against the local artifact refuses on
	// nil rather than removing an anchor it never identified.
	CertPEM []byte
}

// RemoveOutcome reports what a removal found. Found=false is the
// stale-ledger case — an entry whose certificate the store no longer
// holds — and is deliberately not an error: `remove` drops the entry and
// says so (docs/domains/ca.md).
type RemoveOutcome struct {
	// Found reports whether the store held the certificate.
	Found bool
}

// verifyRemovable checks a candidate certificate against BOTH halves of
// the removal contract — the ledger fingerprint and the full marker (CN
// and OU) — and refuses on either failure. source names where the bytes
// came from, so the message says whether the store's copy or the local
// artifact failed.
//
// The fingerprint is re-derived even when the certificate was located by
// fingerprint: that proves the bytes actually hash to the digest the
// store reported beside them.
func verifyRemovable(certPEM []byte, req RemoveRequest, source string) error {
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return newRemovalRefusedError(req.Cube, fmt.Errorf("the %s does not decode as a PEM certificate", source))
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return newRemovalRefusedError(req.Cube, fmt.Errorf("the %s does not parse: %w", source, err))
	}
	sum := sha256.Sum256(cert.Raw)
	if got := hex.EncodeToString(sum[:]); !strings.EqualFold(got, req.Fingerprint) {
		return newRemovalRefusedError(req.Cube,
			fmt.Errorf("the %s has fingerprint %s, but the ledger records %s", source, got, req.Fingerprint))
	}
	if !HasMarker(cert, req.Cube) {
		return newRemovalRefusedError(req.Cube, fmt.Errorf(
			"the %s has CN %q and does not carry the cube-idp marker (want CN %q, OU %s)",
			source, cert.Subject.CommonName, CommonName(req.Cube), MarkerOrganizationalUnit))
	}
	return nil
}

// runFailure folds a tool's stderr into its exit error, so the cause a
// user reads is the tool's own explanation rather than a bare "exit
// status 1".
func runFailure(err error, stderr []byte) error {
	if detail := strings.TrimSpace(string(stderr)); detail != "" {
		return fmt.Errorf("%s: %w", detail, err)
	}
	return err
}
