package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/cube-idp/cube-idp/internal/ca"
)

func newTrustRemoveCmd(deps trustDeps) *cobra.Command {
	return &cobra.Command{
		Use:   "remove <cube>",
		Short: "Remove a cube's CA from this user's OS trust store",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTrustRemove(cmd, deps, args[0])
		},
	}
}

// runTrustRemove undoes one install. It is idempotent by design: a cube
// the ledger never recorded, and a recorded certificate the store no
// longer holds, both report what they found and exit 0. Only a
// certificate that is present but is not this cube's CA is a failure —
// cube-idp deletes nothing it has not identified.
func runTrustRemove(cmd *cobra.Command, deps trustDeps, cube string) error {
	root, err := artifactRoot(deps.homeDir)
	if err != nil {
		return err
	}
	ledgerPath := ca.LedgerPath(root)
	ledger, err := loadLedger(ledgerPath)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	entry, ok := ledger.Find(cube)
	if !ok {
		_, _ = fmt.Fprintf(out, "cube-idp has no record of installing a CA for cube %q\n", cube)
		return nil
	}
	certPath := ca.CertPath(root, cube)
	store, err := trustStore(deps, certPath)
	if err != nil {
		return err
	}
	if entry.Store != store.Name() {
		return ca.NewStoreMismatchError(cube, entry.Store, store.Name())
	}
	outcome, err := removeFromStore(cmd, store, entry, certPath)
	if err != nil {
		return err
	}
	if err := saveLedger(ledgerPath, ledger.Remove(cube)); err != nil {
		return err
	}
	if !outcome.Found {
		_, _ = fmt.Fprintf(out, "no certificate for cube %q in the %s store — dropping the stale ledger entry\n",
			cube, store.Name())
		return nil
	}
	_, _ = fmt.Fprintf(out, "removed cube %q's CA from the %s trust store\n", cube, store.Name())
	return nil
}

// removeFromStore builds the removal request. The local artifact is
// passed alongside its path because the stores identify the certificate
// differently — macOS verifies the copy it pulls out of the keychain,
// p11-kit can only verify this file — and an absent file is passed as
// nil for the store to rule on.
func removeFromStore(cmd *cobra.Command, store ca.Store, entry ca.Entry, certPath string) (ca.RemoveOutcome, error) {
	certPEM, err := readCertFile(entry.Cube, certPath)
	if err != nil {
		return ca.RemoveOutcome{}, err
	}
	return store.Remove(cmd.Context(), ca.RemoveRequest{
		Cube:        entry.Cube,
		Fingerprint: entry.Fingerprint,
		CertPath:    certPath,
		CertPEM:     certPEM,
	})
}
