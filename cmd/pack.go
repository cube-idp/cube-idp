package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	huh "charm.land/huh/v2"
	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"

	"github.com/cube-idp/cube-idp/internal/config"
	"github.com/cube-idp/cube-idp/internal/diag"
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
	packCmd.AddCommand(newPackInstallCmd())
	return packCmd
}

// packCatalog is the single optional-pack catalog behind both init's wizard
// multi-select and `pack install`'s menu (spec WP6): names, published
// versions, descriptions, and (via packCatalogRef) the OCI refs
// config.Default writes.
var packCatalog = []struct {
	Name, Version, Desc string
}{
	{"gitea", "0.1.0", "in-cluster git server"},
	{"argocd", "0.1.0", "delivery UI"},
}

// packCatalogNames lists the catalog names — the substring convention
// filterSelectedPacks matches pack refs against (init.go's optionalPacks).
func packCatalogNames() []string {
	names := make([]string, 0, len(packCatalog))
	for _, p := range packCatalog {
		names = append(names, p.Name)
	}
	return names
}

// packCatalogOptions renders the catalog as huh options in WP6's
// "name@version — description" shape. Option values are catalog names, not
// refs, so init's wizard (which filters config.Default's pack list by name)
// and `pack install` (which maps names to refs) share one option list.
func packCatalogOptions() []huh.Option[string] {
	opts := make([]huh.Option[string], 0, len(packCatalog))
	for _, p := range packCatalog {
		opts = append(opts, huh.NewOption(fmt.Sprintf("%s@%s — %s", p.Name, p.Version, p.Desc), p.Name))
	}
	return opts
}

// packCatalogRef maps a catalog name to the published OCI ref config.Default
// pins ("" for a name outside the catalog — callers pass menu values only).
func packCatalogRef(name string) string {
	for _, p := range packCatalog {
		if p.Name == name {
			return "oci://ghcr.io/cube-idp/packs/" + p.Name + ":" + p.Version
		}
	}
	return ""
}

// Seams for tests — the menu/consent paths need a TTY, so tests override
// these (cmd/down.go's downPromptsAllowed pattern) instead of faking one.
var (
	packPromptsAllowed = ui.PromptsAllowed
	packMenuSelect     = runPackMenu
	packConfirm        = ui.Confirm
)

func newPackInstallCmd() *cobra.Command {
	var file string
	c := &cobra.Command{
		Use:   "install [pack-ref...]",
		Short: "Add packs to cube.yaml (delivered on the next `cube-idp up`)",
		Long: "Append pack refs to cube.yaml's spec.packs. This command only edits the\n" +
			"config file — nothing touches the cluster until the next `cube-idp up`.\n" +
			"With refs as arguments it never prompts (script/CI safe). Bare on a real\n" +
			"terminal it offers a filterable multi-select over the pack catalog.",
		RunE: func(c *cobra.Command, args []string) error {
			refs := args
			if len(refs) == 0 {
				// gh doctrine (spec Decision 4): bare + non-interactive must
				// refuse fast and name the scriptable twin — never hang.
				if !packPromptsAllowed(c.InOrStdin(), c.OutOrStdout()) {
					return diag.New(diag.CodeConfirmRequired,
						"pack install needs pack refs in non-interactive mode",
						"pass refs: cube-idp pack install oci://ghcr.io/cube-idp/packs/<name>:<version>")
				}
				names, err := packMenuSelect(c.InOrStdin(), c.OutOrStdout())
				if err != nil {
					return err
				}
				for _, n := range names {
					refs = append(refs, packCatalogRef(n))
				}
				if len(refs) == 0 {
					fmt.Fprintln(c.OutOrStdout(), "aborted — nothing was changed")
					return nil
				}
				// One summary Confirm (TE-3.2's summary-then-confirm shape).
				ok, err := packConfirm(c.InOrStdin(), c.OutOrStdout(), ui.ConfirmOpts{
					Title:       fmt.Sprintf("Add %d pack(s) to cube.yaml?", len(refs)),
					Description: strings.Join(refs, "\n"),
					Default:     true,
				})
				if err != nil {
					return err
				}
				if !ok {
					fmt.Fprintln(c.OutOrStdout(), "aborted — nothing was changed")
					return nil
				}
			}
			if err := packInstallRefs(c.OutOrStdout(), file, refs); err != nil {
				return err
			}
			if len(args) == 0 {
				// Scriptable-twin hint after an accepted prompt (spec TE-3.2).
				fmt.Fprintln(c.OutOrStdout(), "  "+th.Dim.Render("hint: cube-idp pack install "+strings.Join(refs, " ")))
			}
			return nil
		},
	}
	c.Flags().StringVarP(&file, "file", "f", "cube.yaml", "path to cube.yaml")
	return c
}

// runPackMenu is the TTY half of `pack install` (spec WP6): a filterable huh
// MultiSelect over the pack catalog. Callers gate on ui.PromptsAllowed; this
// function assumes the terminal is available.
func runPackMenu(in io.Reader, out io.Writer) ([]string, error) {
	var names []string
	form := huh.NewForm(huh.NewGroup(
		huh.NewMultiSelect[string]().
			Title("Packs to install").
			Options(packCatalogOptions()...).
			Filterable(true).
			Value(&names),
	)).WithInput(in).WithOutput(out).WithAccessible(os.Getenv("ACCESSIBLE") != "")
	if err := form.Run(); err != nil {
		return nil, err
	}
	return names, nil
}

// packInstallRefs appends the non-duplicate refs to file's spec.packs and
// writes it back with init's writer (sigs.k8s.io/yaml + 0o644). The candidate
// is validated by config.Load — the exact schema + cross-field checks `up`
// applies (non-empty ref, argocd-engine redundancy) — via a temp file in the
// same directory before it replaces the original, so a rejected ref leaves
// cube.yaml untouched.
func packInstallRefs(out io.Writer, file string, refs []string) error {
	cube, err := config.Load(file)
	if err != nil {
		return err
	}
	p := ui.NewFor(out)
	existing := make(map[string]bool, len(cube.Spec.Packs))
	for _, pr := range cube.Spec.Packs {
		existing[pr.Ref] = true
	}
	var added []string
	for _, ref := range refs {
		if existing[ref] {
			p.Warn("already in cube.yaml: %s — skipped", ref)
			continue
		}
		cube.Spec.Packs = append(cube.Spec.Packs, config.PackRef{Ref: ref})
		existing[ref] = true
		added = append(added, ref)
	}
	if len(added) == 0 {
		fmt.Fprintln(out, "nothing to add — cube.yaml unchanged")
		return nil
	}
	raw, err := yaml.Marshal(cube)
	if err != nil {
		return err
	}
	tmp := file + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	if _, err := config.Load(tmp); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, file); err != nil {
		os.Remove(tmp)
		return err
	}
	for _, ref := range added {
		p.Step("pack", "added %s", ref)
	}
	if p.Styled() {
		fmt.Fprintln(out, th.Dim.Render("next: cube-idp up"))
	} else {
		fmt.Fprintln(out, "next: cube-idp up")
	}
	return nil
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
