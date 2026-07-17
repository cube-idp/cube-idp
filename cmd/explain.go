package cmd

import (
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/cube-idp/cube-idp/internal/diag"
)

// newExplainCmd is the lookup half of the stable-code contract (rustc
// --explain pattern, spec WP8): every CUBE-xxxx a diagnosis prints can be
// resolved offline, which is what lets the TE-2.3 box footer advertise it.
// Output is plain text; `-o json` is future work (recorded in the plan).
func newExplainCmd() *cobra.Command {
	var list bool
	c := &cobra.Command{
		Use:   "explain CUBE-XXXX",
		Short: "Explain a cube-idp diagnostic code",
		Long: "Explain prints what a CUBE-XXXX diagnostic code means: its summary and\n" +
			"the documented meaning of its numeric range. The codes are stable and\n" +
			"append-only, so an explanation stays valid across releases.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			out := c.OutOrStdout()
			if list {
				tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
				for _, code := range diag.AllCodes() {
					d, _ := diag.Describe(code)
					fmt.Fprintf(tw, "%s\t%s\n", code, d.Summary)
				}
				return tw.Flush()
			}
			if len(args) == 0 {
				return diag.New(diag.CodeBadFlagValue,
					"explain needs a diagnostic code",
					"run `cube-idp explain CUBE-XXXX` with a code from a diagnosis, or `cube-idp explain --list`")
			}
			code := diag.Code(strings.ToUpper(args[0]))
			d, ok := diag.Describe(code)
			if !ok {
				return diag.New(diag.CodeBadFlagValue,
					fmt.Sprintf("unknown diagnostic code %q", args[0]),
					"see internal/diag/codes.go ranges; run cube-idp explain --list")
			}
			fmt.Fprintf(out, "%s  %s\n", code, d.Summary)
			if m := diag.RangeMeaning(code); m != "" {
				fmt.Fprintf(out, "range: %s\n", m)
			}
			if d.Detail != "" {
				fmt.Fprintf(out, "\n%s\n", d.Detail)
			}
			if d.Remediation != "" {
				fmt.Fprintf(out, "fix:   %s\n", d.Remediation)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&list, "list", false, "list every diagnostic code with its summary")
	return c
}
