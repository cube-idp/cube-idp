package cli

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"

	"github.com/spf13/cobra"

	"github.com/cube-idp/cube-idp/internal/config"
)

func newInitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Scaffold the config document if absent and validate it",
		RunE:  runInit,
	}
	cmd.Flags().String("name", "",
		"cube name for a scaffolded config (must match metadata.name when the file already exists)")
	return cmd
}

// runInit is config-only: scaffold-if-absent → load → report, exit 0.
// It never provisions and never touches a kubeconfig — that is create's
// job (operator split, 2026-08-03). Re-runs are idempotent: an existing
// config is loaded, validated, and reported.
func runInit(cmd *cobra.Command, _ []string) error {
	path, _ := cmd.Flags().GetString("config")
	nameFlag, _ := cmd.Flags().GetString("name")

	scaffolded, err := scaffoldIfAbsent(cmd.OutOrStdout(), path, nameFlag)
	if err != nil {
		return err
	}
	cfg, err := config.LoadFile(path)
	if err != nil {
		return err
	}
	// Mismatch only: a --name equal to the document's metadata.name is a
	// no-op, keeping init --name <x> idempotent. Flags never mutate an
	// existing config.
	if nameFlag != "" && cfg.Name != nameFlag {
		return config.NewNameConflictError(path, cfg.Name, nameFlag)
	}
	if scaffolded {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), `run "cube-idp create" to provision the cluster`)
		return nil
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "config %s exists — cube %q\n", path, cfg.Name)
	return nil
}

// scaffoldIfAbsent creates the config document when path does not exist:
// metadata.name from nameFlag if set, else a generated docker-style name.
// The bool reports whether a file was created. An existing file is left
// untouched, and any other stat failure falls through to the loader,
// which reports it with path context. A concurrent creation between the
// check and the O_EXCL write surfaces as the scaffold's own
// already-exists error rather than clobbering the file.
func scaffoldIfAbsent(stdout io.Writer, path, nameFlag string) (bool, error) {
	if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	name := nameFlag
	if name == "" {
		name = config.GenerateName()
	}
	if err := config.ScaffoldFile(path, name); err != nil {
		return false, err
	}
	_, _ = fmt.Fprintf(stdout, "scaffolded %s — cube %q\n", path, name)
	return true, nil
}
