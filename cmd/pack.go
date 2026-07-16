package cmd

import (
	"context"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cube-idp/cube-idp/internal/oci"
	"github.com/cube-idp/cube-idp/internal/pack"
	"github.com/cube-idp/cube-idp/internal/ui"
)

func newPackCmd() *cobra.Command {
	packCmd := &cobra.Command{Use: "pack", Short: "Work with cube-idp packs"}

	var alsoTags []string
	push := &cobra.Command{
		Use:   "push <dir> <oci-ref>",
		Short: "Publish a pack directory as an OCI artifact",
		Long: "Publish a pack source directory (pack.cue + manifests/ + chart.yaml) to an OCI\n" +
			"registry in the shape `cube-idp` itself pulls (oci:// pack refs in cube.yaml).\n" +
			"If <oci-ref> carries no :tag, the pack's version from pack.cue is used.\n" +
			"Auth is the ambient docker credential chain (docker login).",
		Args: cobra.ExactArgs(2),
		// RunPipelineStatic owns the whole RunE body (Task R3): pack push is
		// a short static command, never a live step-tree.
		RunE: func(c *cobra.Command, args []string) error {
			return ui.RunPipelineStatic(c.Context(), "pack", c.OutOrStdout(),
				func(ctx context.Context, con *ui.Console) error {
					con.Start("pack", "")
					dir, ref := args[0], args[1]
					if !refHasTag(ref) {
						// Tag defaulting: reuse pack.Fetch on the local dir
						// (it ignores cacheDir for local paths) instead of
						// re-parsing CUE.
						p, err := pack.Fetch(ctx, dir, "")
						if err != nil {
							return err
						}
						ref = ref + ":" + p.Version
					}

					digest, err := oci.PushPackDir(ctx, dir, ref, alsoTags...)
					if err != nil {
						return err
					}
					con.Step("pack", "pushed %s@%s", ref, digest)
					return nil
				})
		},
	}
	push.Flags().StringSliceVar(&alsoTags, "also-tag", nil,
		"additional tags for the same pushed artifact (e.g. latest); repeatable")
	packCmd.AddCommand(push)
	return packCmd
}

// refHasTag reports whether the oci:// ref already carries a :tag or
// @digest. Only the last path segment is inspected, so a port in the host
// (oci://127.0.0.1:5000/packs/demo) is never mistaken for a tag. A ref
// without the oci:// prefix returns true — no tag is appended, and
// PushPackDir rejects it with the canonical CUBE-4015 message.
func refHasTag(ref string) bool {
	rest, ok := strings.CutPrefix(ref, "oci://")
	if !ok {
		return true
	}
	last := rest
	if i := strings.LastIndexByte(rest, '/'); i != -1 {
		last = rest[i+1:]
	}
	return strings.ContainsAny(last, ":@")
}
