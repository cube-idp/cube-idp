package ca

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
)

// Named trust_p11kit.go rather than trust_linux.go for the same reason
// its macOS sibling avoids _darwin: an implicit GOOS constraint would
// keep half the drivers out of the hermetic gate.
const (
	// storeNameP11Kit is the ledger's store value for the p11-kit user
	// anchor store. User-visible and frozen once written.
	storeNameP11Kit = "p11-kit"
	// trustTool is p11-kit's command-line front end.
	trustTool = "trust"
)

// p11KitStore is the p11-kit user anchor store, reached through trust(1).
type p11KitStore struct{ run Runner }

// NewP11KitStore returns the Linux user-scope anchor store, driven by
// run. On mainstream distributions no user-writable anchor store is
// configured, so its operations ordinarily fail — by design, that
// failure carries the per-distro manual instructions, which are the
// ordinary Linux experience (docs/domains/ca.md).
func NewP11KitStore(run Runner) Store { return p11KitStore{run: run} }

func (s p11KitStore) Name() string { return storeNameP11Kit }

// Install probes trust(1) and then stores the anchor. The probe
// separates "p11-kit is not installed" from "p11-kit has nowhere to
// write", which need completely different remedies.
func (s p11KitStore) Install(ctx context.Context, cube, certPath string) error {
	if _, _, err := s.run(ctx, trustTool, "list", "--filter=ca-anchors"); err != nil {
		return newP11KitMissingError(cube, certPath, err)
	}
	if _, stderr, err := s.run(ctx, trustTool, "anchor", "--store", certPath); err != nil {
		return newP11KitAnchorError(
			fmt.Sprintf("cannot store the CA at %s as a user trust anchor", certPath),
			cube, certPath, runFailure(err, stderr))
	}
	return nil
}

// Remove verifies the LOCAL artifact rather than a copy pulled out of
// the store: `trust anchor --remove` takes a file and p11-kit offers no
// way to read an anchor back, so the check proves the file is this
// cube's CA — not that the store held that exact certificate. The
// asymmetry with the macOS driver is recorded in docs/domains/ca.md.
func (s p11KitStore) Remove(ctx context.Context, req RemoveRequest) (RemoveOutcome, error) {
	if len(req.CertPEM) == 0 {
		return RemoveOutcome{}, newTrustStoreError(
			fmt.Sprintf("cannot remove cube %q's CA: there is no certificate at %s to verify it against",
				req.Cube, req.CertPath),
			fmt.Sprintf(`run "cube-idp bootstrap" to re-emit the certificate and retry, or %s`,
				manualRemoveLinux(req.Cube)), nil)
	}
	if err := verifyRemovable(req.CertPEM, req, "certificate at "+req.CertPath); err != nil {
		return RemoveOutcome{}, err
	}
	_, stderr, err := s.run(ctx, trustTool, "anchor", "--remove", req.CertPath)
	switch {
	case errors.Is(err, exec.ErrNotFound):
		return RemoveOutcome{}, newP11KitMissingError(req.Cube, req.CertPath, err)
	case err != nil:
		return RemoveOutcome{}, newTrustStoreError(
			fmt.Sprintf("cannot remove cube %q's CA from the p11-kit anchor store", req.Cube),
			manualRemoveLinux(req.Cube), runFailure(err, stderr))
	}
	// p11-kit cannot report that an anchor was absent: trust(1) exits 0
	// whether or not the anchor was there, so a removal of an
	// already-absent anchor is indistinguishable from a real one. This
	// store therefore never reports Found=false — the stale-entry message
	// runTrustRemove prints is effectively macOS-only.
	return RemoveOutcome{Found: true}, nil
}

// newP11KitMissingError reports trust(1) itself being unavailable. The
// remedy is either to install p11-kit or to skip it entirely, so both
// are offered.
func newP11KitMissingError(cube, certPath string, cause error) error {
	return newTrustStoreError(
		fmt.Sprintf("cannot use the Linux trust store: %s(1) (p11-kit) is not installed", trustTool),
		"install p11-kit (apt install p11-kit / dnf install p11-kit), or install the CA by hand:\n"+
			systemInstallLinux(cube, certPath), cause)
}

// newP11KitAnchorError reports a `trust anchor` call that failed. On
// mainstream distributions the reason is that no user-writable anchor
// store is configured — the expected outcome, not an edge case — so the
// remediation is always the full system-wide instruction rather than a
// retry hint. The cause line carries p11-kit's own words.
func newP11KitAnchorError(summary, cube, certPath string, cause error) error {
	return newTrustStoreError(summary,
		fmt.Sprintf("cube-idp emitted the CA at %s. Install it system-wide:\n%s\n"+
			"Firefox and Chrome keep their own stores — import it there separately.",
			certPath, systemInstallLinux(cube, certPath)), cause)
}

// systemInstallLinux is the per-distro manual instruction. It is a sudo
// cp rather than a p11-kit retry because the anchors curl, OpenSSL and
// Go binaries actually read (/etc/ssl/certs, /etc/pki) are regenerated
// by root-only tools that a user-scope p11-kit token would not feed.
func systemInstallLinux(cube, certPath string) string {
	name := systemCertFileName(cube)
	return fmt.Sprintf(
		"    Debian/Ubuntu: sudo cp %s /usr/local/share/ca-certificates/%s && sudo update-ca-certificates\n"+
			"    Fedora/RHEL:   sudo cp %s /etc/pki/ca-trust/source/anchors/%s && sudo update-ca-trust",
		certPath, name, certPath, name)
}

// manualRemoveLinux is the by-hand removal remedy, and the reason
// Install's destination filename carries the cube name: the system
// anchor directories are shared, so two cubes' CAs must not both land as
// ca.crt.
func manualRemoveLinux(cube string) string {
	name := systemCertFileName(cube)
	return fmt.Sprintf("remove it by hand — `sudo rm -f /usr/local/share/ca-certificates/%s "+
		"/etc/pki/ca-trust/source/anchors/%s` followed by your distribution's update command — "+
		"then delete the entry from ~/%s/%s", name, name, DirName, LedgerFileName)
}

// systemCertFileName is the per-cube filename a hand-installed CA takes
// in the shared system anchor directories.
func systemCertFileName(cube string) string {
	return fmt.Sprintf("cube-idp-%s.crt", cube)
}
