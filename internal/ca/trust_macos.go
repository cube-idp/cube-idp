package ca

import (
	"context"
	"fmt"
	"strings"
)

// The file is deliberately NOT named trust_darwin.go: Go applies an
// implicit GOOS constraint from that filename, and a driver that only
// compiles on macOS could never run its half of the hermetic gate.
// Nothing here executes anything — the driver builds argv and parses
// text, and the Runner is injected.
const (
	// storeNameMacOS is the ledger's store value for the login keychain.
	// It is user-visible and frozen once a machine has a ledger on disk.
	storeNameMacOS = "macos-login"
	// securityTool is macOS's own keychain tool.
	securityTool = "security"
)

// macOSStore is the login keychain, reached through security(1).
type macOSStore struct{ run Runner }

// NewMacOSStore returns the macOS login keychain store, driven by run.
// Trust goes into the user's own keychain — the admin store is never
// touched, so no operation needs sudo.
func NewMacOSStore(run Runner) Store { return macOSStore{run: run} }

func (s macOSStore) Name() string { return storeNameMacOS }

// Install adds the certificate as a user-scope trusted root.
func (s macOSStore) Install(ctx context.Context, _, certPath string) error {
	keychain, err := s.loginKeychain(ctx)
	if err != nil {
		return newMacOSKeychainError(manualInstallMacOS(certPath), err)
	}
	// -d is omitted deliberately: it would target the admin certificate
	// store, and v0 trust is user scope only. -r trustRoot is the
	// default, passed explicitly so the argv is self-documenting.
	if _, stderr, err := s.run(ctx, securityTool,
		"add-trusted-cert", "-r", "trustRoot", "-k", keychain, certPath); err != nil {
		return newTrustStoreError(
			fmt.Sprintf("cannot add the CA at %s to the macOS login keychain", certPath),
			manualInstallMacOS(certPath), runFailure(err, stderr))
	}
	return nil
}

// Remove deletes the cube's CA from the login keychain, after verifying
// the certificate the keychain itself holds.
func (s macOSStore) Remove(ctx context.Context, req RemoveRequest) (RemoveOutcome, error) {
	keychain, err := s.loginKeychain(ctx)
	if err != nil {
		return RemoveOutcome{}, newMacOSKeychainError(manualRemoveMacOS(req.Cube), err)
	}
	found, err := s.find(ctx, keychain, req)
	switch {
	case err != nil:
		return RemoveOutcome{}, err
	case found == nil:
		return RemoveOutcome{Found: false}, nil
	}
	if err := verifyRemovable(found.certPEM, req, "keychain certificate"); err != nil {
		return RemoveOutcome{}, err
	}
	// -t drops the user trust settings along with the certificate, so no
	// separate remove-trusted-cert call is needed.
	if _, stderr, err := s.run(ctx, securityTool,
		"delete-certificate", "-Z", found.sha256, "-t", keychain); err != nil {
		return RemoveOutcome{}, newTrustStoreError(
			fmt.Sprintf("cannot delete cube %q's CA from the macOS login keychain", req.Cube),
			manualRemoveMacOS(req.Cube), runFailure(err, stderr))
	}
	if err := s.confirmGone(ctx, keychain, req); err != nil {
		return RemoveOutcome{}, err
	}
	return RemoveOutcome{Found: true}, nil
}

// confirmGone re-runs the search after a delete. security(1)'s exit code
// is not evidence: delete-certificate exits 0 whether it deleted the
// certificate or merely printed that it could not find it, so absence is
// established by looking, never by the return code.
func (s macOSStore) confirmGone(ctx context.Context, keychain string, req RemoveRequest) error {
	found, err := s.find(ctx, keychain, req)
	if err != nil {
		return err
	}
	if found != nil {
		return newTrustStoreError(
			fmt.Sprintf("cube %q's CA is still in the macOS login keychain after deleting it", req.Cube),
			manualRemoveMacOS(req.Cube), nil)
	}
	return nil
}

// loginKeychain resolves the user's login keychain and, in the same
// call, proves security(1) exists and runs — the probe and the answer
// are one command. The error is uncoded on purpose: only the calling
// verb knows whether the manual remedy is to add or to delete a
// certificate, so it is the verb that wraps this into CUBE-CA-004.
func (s macOSStore) loginKeychain(ctx context.Context) (string, error) {
	stdout, stderr, err := s.run(ctx, securityTool, "login-keychain")
	if err != nil {
		return "", fmt.Errorf("%s login-keychain: %w", securityTool, runFailure(err, stderr))
	}
	// The path is printed indented and quoted.
	path := strings.Trim(strings.TrimSpace(string(stdout)), `"`)
	if path == "" {
		return "", fmt.Errorf("%s login-keychain named no keychain", securityTool)
	}
	return path, nil
}

// keychainCert is one (SHA-256, PEM) pair parsed out of
// find-certificate's output.
type keychainCert struct {
	sha256  string
	certPEM []byte
}

// find locates the cube's CA in a keychain by ledger fingerprint,
// returning nil when the keychain holds no such certificate. -a is
// mandatory: without it a miss exits 44 with an error on stderr, while
// with it a miss is an ordinary empty result on a zero exit.
func (s macOSStore) find(ctx context.Context, keychain string, req RemoveRequest) (*keychainCert, error) {
	stdout, stderr, err := s.run(ctx, securityTool,
		"find-certificate", "-a", "-p", "-Z", "-c", CommonName(req.Cube), keychain)
	if err != nil {
		return nil, newTrustStoreError(
			fmt.Sprintf("cannot search the macOS login keychain for cube %q's CA", req.Cube),
			manualRemoveMacOS(req.Cube), runFailure(err, stderr))
	}
	certs := parseKeychainCerts(stdout)
	for i := range certs {
		if strings.EqualFold(certs[i].sha256, req.Fingerprint) {
			return &certs[i], nil
		}
	}
	return nil, nil
}

// parseKeychainCerts splits `find-certificate -a -p -Z` output into
// (SHA-256, PEM) pairs. Each certificate is reported as a "SHA-256
// hash:" line, a "SHA-1 hash:" line, then its PEM block; the SHA-1 line
// is ignored, because delete-certificate accepts the SHA-256.
func parseKeychainCerts(stdout []byte) []keychainCert {
	const (
		hashPrefix = "SHA-256 hash: "
		pemBegin   = "-----BEGIN CERTIFICATE-----"
		pemEnd     = "-----END CERTIFICATE-----"
	)
	var (
		certs   []keychainCert
		current keychainCert
		block   []string
	)
	for _, line := range strings.Split(string(stdout), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, hashPrefix):
			current = keychainCert{sha256: strings.TrimPrefix(line, hashPrefix)}
		case line == pemBegin:
			block = []string{line}
		case block != nil:
			block = append(block, line)
			if line == pemEnd {
				current.certPEM = []byte(strings.Join(block, "\n") + "\n")
				certs = append(certs, current)
				block = nil
			}
		}
	}
	return certs
}

// newMacOSKeychainError reports a login keychain that could not be
// resolved — security(1) missing, or unable to name one. Both verbs
// raise it and each supplies its own remedy, because "do it by hand"
// means opposite things to install and remove.
func newMacOSKeychainError(remediation string, cause error) error {
	return newTrustStoreError(
		fmt.Sprintf("cannot use the macOS login keychain: %s(1) is unavailable", securityTool),
		remediation, cause)
}

// manualInstallMacOS is the by-hand remedy when security(1) cannot do it.
func manualInstallMacOS(certPath string) string {
	return fmt.Sprintf(`install the CA by hand: open Keychain Access, drag %s into the "login" keychain, `+
		`then set it to "Always Trust"`, certPath)
}

// manualRemoveMacOS is the by-hand remedy for the removal half. It names
// the certificate by its marker CN, which is what Keychain Access shows.
func manualRemoveMacOS(cube string) string {
	return fmt.Sprintf(`remove the CA by hand: open Keychain Access, select the "login" keychain, `+
		"find the certificate named %q and delete it", CommonName(cube))
}
