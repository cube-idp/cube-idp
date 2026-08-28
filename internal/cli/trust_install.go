package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/cube-idp/cube-idp/internal/ca"
)

func newTrustInstallCmd(deps trustDeps) *cobra.Command {
	return &cobra.Command{
		Use:   "install <cube>",
		Short: "Install a cube's CA into this user's OS trust store",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTrustInstall(cmd, deps, args[0])
		},
	}
}

// runTrustInstall consumes the certificate bootstrap emitted. It never
// mints and never falls back to reading the cluster Secret: a second
// source of truth for the CA is what the reuse contract exists to
// prevent, and a cluster read would end the verb's hermetic testability.
//
// The verb is deliberately explicit — on macOS it is interactive — and
// bootstrap never calls it: silent trust anchors are not something
// cube-idp installs on a user's behalf.
func runTrustInstall(cmd *cobra.Command, deps trustDeps, cube string) error {
	root, err := artifactRoot(deps.homeDir)
	if err != nil {
		return err
	}
	certPath := ca.CertPath(root, cube)
	store, err := trustStore(deps, certPath)
	if err != nil {
		return err
	}
	fingerprint, err := artifactFingerprint(cube, certPath)
	if err != nil {
		return err
	}
	// The ledger is read before the store is touched, so a hand-edited
	// ledger fails the verb instead of leaving an installed certificate
	// nothing records.
	ledgerPath := ca.LedgerPath(root)
	ledger, err := loadLedger(ledgerPath)
	if err != nil {
		return err
	}
	if err := store.Install(cmd.Context(), cube, certPath); err != nil {
		return err
	}
	entry := ca.Entry{
		Cube:        cube,
		Fingerprint: fingerprint,
		Store:       store.Name(),
		Date:        deps.now().UTC().Format(dateFormat),
	}
	if err := saveLedger(ledgerPath, ledger.Upsert(entry)); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "installed cube %q's CA from %s into the %s trust store\n",
		cube, certPath, store.Name())
	return nil
}

// artifactFingerprint reads the emitted certificate and derives the
// ledger's identity for it. Both failures are the install operation
// failing before it reaches the store, so both are CUBE-CA-004 — a
// precondition of a verb does not earn its own catalog entry.
func artifactFingerprint(cube, certPath string) (string, error) {
	certPEM, err := readCertFile(cube, certPath)
	switch {
	case err != nil:
		return "", err
	case certPEM == nil:
		return "", ca.NewMissingArtifactError(cube, certPath, nil)
	}
	fingerprint, err := ca.Fingerprint(certPEM)
	if err != nil {
		return "", ca.NewUnusableArtifactError(cube, certPath, err)
	}
	return fingerprint, nil
}
