package cli

import (
	"fmt"
	"io"
	"os"
	"runtime"
	"time"

	"github.com/spf13/cobra"

	"github.com/cube-idp/cube-idp/internal/ca"
)

// trustDeps are the trust group's injected seams. Home resolution is
// injected rather than read from the environment inside the domain —
// internal/ca deliberately overrides internal/cluster's in-domain
// os.UserHomeDir call (docs/domains/ca.md) — so no test can touch a real
// $HOME. The clock and the OS name are injected for the same reason the
// mint path injects Now: the recorded date and the store choice must be
// assertable.
type trustDeps struct {
	homeDir func() (string, error)
	now     func() time.Time
	goos    string
	run     ca.Runner
}

// defaultTrustDeps is the production composition of the trust group.
func defaultTrustDeps() trustDeps {
	return trustDeps{homeDir: os.UserHomeDir, now: time.Now, goos: runtime.GOOS, run: defaultRunner}
}

func newTrustCmd(deps trustDeps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "trust",
		Short: "Manage the CA certificates cube-idp installs into OS trust stores",
		// The group repeats the root's rendering contract so a test that
		// runs the subtree standalone renders exactly as production does.
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	cmd.AddCommand(newTrustListCmd(deps), newTrustInstallCmd(deps), newTrustRemoveCmd(deps))
	return cmd
}

func newTrustListCmd(deps trustDeps) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "Print the trust ledger",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runTrustList(cmd, deps)
		},
	}
}

// runTrustList prints the ledger and exits 0 whether or not it records
// anything: a machine with no CA installed is a finding, not a failure —
// the runStatus doctrine.
func runTrustList(cmd *cobra.Command, deps trustDeps) error {
	root, err := artifactRoot(deps.homeDir)
	if err != nil {
		return err
	}
	ledger, err := loadLedger(ca.LedgerPath(root))
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	if len(ledger.Entries) == 0 {
		_, _ = fmt.Fprintln(out, "no CA certificates installed by cube-idp")
		return nil
	}
	writeTrustList(out, ledger.Entries)
	return nil
}

// writeTrustList renders the entries as aligned columns, in the order
// the ledger holds them (Marshal keeps the file sorted by cube). The
// variable-width columns follow the widest value, so a long cube or
// store name cannot ragged the table; the last column is never padded,
// so no line carries trailing whitespace.
func writeTrustList(w io.Writer, entries []ca.Entry) {
	cube, store := len("CUBE"), len("STORE")
	for _, e := range entries {
		cube = max(cube, len(e.Cube))
		store = max(store, len(e.Store))
	}
	// The fingerprint column is fixed: SHA-256 hex is always 64 characters.
	format := fmt.Sprintf("%%-%ds  %%-64s  %%-%ds  %%s\n", cube, store)
	_, _ = fmt.Fprintf(w, format, "CUBE", "FINGERPRINT (SHA-256)", "STORE", "INSTALLED")
	for _, e := range entries {
		_, _ = fmt.Fprintf(w, format, e.Cube, e.Fingerprint, e.Store, e.Date)
	}
}
