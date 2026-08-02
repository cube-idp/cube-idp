package cli

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/cube-idp/cube-idp/internal/cubeerr"
)

// Execute runs the CLI and returns the process exit code. It is the ONLY
// place errors are rendered: coded errors print code, summary, cause
// detail, and remediation to stderr.
func Execute(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	return execute(ctx, newRootCmd(defaultProvisioner), args, stdout, stderr)
}

// execute is Execute minus composition, so tests run a command tree
// built with an injected provisioner factory.
func execute(ctx context.Context, root *cobra.Command, args []string, stdout, stderr io.Writer) int {
	root.SetArgs(args)
	root.SetOut(stdout)
	root.SetErr(stderr)

	if err := root.ExecuteContext(ctx); err != nil {
		printError(stderr, err)
		return cubeerr.ExitCode(err)
	}
	return cubeerr.ExitSuccess
}

func printError(w io.Writer, err error) {
	var coded *cubeerr.Coded
	if !errors.As(err, &coded) {
		_, _ = fmt.Fprintf(w, "✗ error: %v\n", err)
		return
	}
	_, _ = fmt.Fprintf(w, "✗ %s: %s\n", coded.Code, coded.Summary)
	if cause := coded.Unwrap(); cause != nil {
		_, _ = fmt.Fprintf(w, "    %v\n", cause)
	}
	if coded.Remediation != "" {
		_, _ = fmt.Fprintf(w, "  → %s\n", coded.Remediation)
	}
}
