package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	huh "charm.land/huh/v2"
	"github.com/spf13/cobra"

	"github.com/cube-idp/cube-idp/internal/cfgload"
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
	// P2 (Phase 5): the packs-repo CI toolchain — publish + index build/push.
	packCmd.AddCommand(newPackPublishCmd(), newPackIndexCmd())
	// P6 (Phase 5): the index-backed remote catalog surfaces.
	packCmd.AddCommand(newPackListCmd(), newPackSearchCmd())
	return packCmd
}

// packCatalog is the BUILT-IN optional-pack catalog — since P6 the offline
// FALLBACK (never deleted) behind loadPackCatalog: when the published index
// artifact is unreachable, init's wizard and `pack install`'s menu offer
// exactly this pre-P6 list. Wording stays in sync with the packs'
// pack.cue descriptions (P1).
var packCatalog = []struct {
	Name, Version, Desc string
}{
	{"gitea", "0.2.0", "in-cluster git server"},
	{"argocd", "0.2.0", "delivery UI"},
}

// packCatalogNames lists the BUILT-IN catalog names — the substring
// convention filterSelectedPacks matches pack refs against (init.go's
// optionalPacks), and applyWizardToCube's fence between "filter the default
// profile" (built-in names) and "append a discovered pack" (remote-only
// names). Deliberately static: the default profile's members do not grow
// when the remote index does.
func packCatalogNames() []string {
	names := make([]string, 0, len(packCatalog))
	for _, p := range packCatalog {
		names = append(names, p.Name)
	}
	return names
}

// catalogEntry is one resolved pack-catalog row — remote index entry or
// built-in fallback — the single shape every P6 consumer (menu, wizard,
// list, search) renders from.
type catalogEntry struct {
	Name, Version, Desc, Ref string
}

// builtinCatalogEntries maps packCatalog into entries, deriving the same
// published OCI refs config.Default pins.
func builtinCatalogEntries() []catalogEntry {
	entries := make([]catalogEntry, 0, len(packCatalog))
	for _, p := range packCatalog {
		entries = append(entries, catalogEntry{
			Name:    p.Name,
			Version: p.Version,
			Desc:    p.Desc,
			Ref:     "oci://ghcr.io/cube-idp/packs/" + p.Name + ":" + p.Version,
		})
	}
	return entries
}

// catalogFetchTimeout bounds the index pull inside loadPackCatalog: the
// catalog gates interactive surfaces (menu, wizard), so a black-hole
// network must degrade to the built-in fallback in seconds, not hang until
// the OS TCP timeout. Pack pulls proper (`up`) keep their unbounded
// context — big artifacts may legitimately take long.
const catalogFetchTimeout = 10 * time.Second

// loadPackCatalog resolves the catalog every menu and listing consumes: the
// remote index when reachable (pack.FetchCatalog — 24h cache,
// CUBE_IDP_PACK_INDEX override), else the built-in fallback announced with
// a single advisory line. Callers invoke it once per command so that line
// never repeats; offline behavior is byte-for-byte the pre-P6 catalog.
func loadPackCatalog(ctx context.Context, out io.Writer) []catalogEntry {
	ctx, cancel := context.WithTimeout(ctx, catalogFetchTimeout)
	defer cancel()
	cat, err := pack.FetchCatalog(ctx)
	if err != nil {
		ui.NewFor(out).Warn("catalog: using built-in list (index unreachable: %v)", err)
		return builtinCatalogEntries()
	}
	entries := make([]catalogEntry, 0, len(cat.Packs))
	for _, e := range cat.Packs {
		entries = append(entries, catalogEntry{Name: e.Name, Version: e.Version, Desc: e.Description, Ref: e.Ref})
	}
	return entries
}

// catalogOptions renders entries as huh options in WP6's
// "name@version — description" shape. Option values are catalog names, not
// refs, so init's wizard (which filters config.Default's pack list by name)
// and `pack install` (which maps names to refs) share one option list.
func catalogOptions(entries []catalogEntry) []huh.Option[string] {
	opts := make([]huh.Option[string], 0, len(entries))
	for _, e := range entries {
		opts = append(opts, huh.NewOption(fmt.Sprintf("%s@%s — %s", e.Name, e.Version, e.Desc), e.Name))
	}
	return opts
}

// catalogRef maps a catalog name to its entry's OCI ref ("" for a name
// outside the catalog — callers pass menu values only).
func catalogRef(entries []catalogEntry, name string) string {
	for _, e := range entries {
		if e.Name == name {
			return e.Ref
		}
	}
	return ""
}

// printCatalogRows renders entries as name/version/description rows —
// `spoke list`'s fixed-width column style.
func printCatalogRows(out io.Writer, entries []catalogEntry) {
	for _, e := range entries {
		fmt.Fprintf(out, "%-20s %-10s %s\n", e.Name, e.Version, e.Desc)
	}
}

func newPackListCmd() *cobra.Command {
	var available bool
	c := &cobra.Command{
		Use:   "list",
		Short: "List packs available from the remote catalog",
		Long: "List every pack the published catalog index offers, one\n" +
			"name/version/description row per pack. The index is cached for 24h;\n" +
			"when it is unreachable the built-in list is shown instead (with a\n" +
			"notice). Packs configured for THIS cube live in cube.yaml (spec.packs).",
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			if !available {
				// The bare form is reserved (a future task may list the
				// cube's own packs); today the command exists for the
				// catalog, so refuse with the exact spelling to use.
				return diag.New(diag.CodeBadFlagValue,
					"pack list requires --available (the remote catalog)",
					"run `cube-idp pack list --available`; packs configured for this cube are listed in cube.yaml under spec.packs")
			}
			printCatalogRows(c.OutOrStdout(), loadPackCatalog(c.Context(), c.OutOrStdout()))
			return nil
		},
	}
	c.Flags().BoolVar(&available, "available", false, "list every pack in the remote catalog")
	return c
}

func newPackSearchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "search <term>",
		Short: "Search the pack catalog by name and description",
		Long: "Case-insensitively match <term> against the name and description of\n" +
			"every pack in the remote catalog (built-in fallback when the index is\n" +
			"unreachable) and print the matching name/version/description rows.",
		Args: cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			term := strings.ToLower(args[0])
			var matched []catalogEntry
			for _, e := range loadPackCatalog(c.Context(), c.OutOrStdout()) {
				if strings.Contains(strings.ToLower(e.Name), term) ||
					strings.Contains(strings.ToLower(e.Desc), term) {
					matched = append(matched, e)
				}
			}
			if len(matched) == 0 {
				fmt.Fprintf(c.OutOrStdout(), "no packs match %q\n", args[0])
				return nil
			}
			printCatalogRows(c.OutOrStdout(), matched)
			return nil
		},
	}
}

// Seams for tests — the menu/consent paths need a TTY, so tests override
// these (cmd/down.go's downPromptsAllowed pattern) instead of faking one.
var (
	packPromptsAllowed = ui.PromptsAllowed
	packMenuSelect     = runPackMenu
	packConfirm        = ui.Confirm
)

func newPackInstallCmd() *cobra.Command {
	var file, via string
	c := &cobra.Command{
		Use:   "install [pack-ref...]",
		Short: "Add packs to cube.yaml (delivered on the next `cube-idp up`)",
		Long: "Append pack refs to cube.yaml's spec.packs. This command only edits the\n" +
			"config file — nothing touches the cluster until the next `cube-idp up`.\n" +
			"With refs as arguments it never prompts (script/CI safe). Bare on a real\n" +
			"terminal it offers a filterable multi-select over the pack catalog.\n" +
			"--via repo (P7) delivers the pack as an editable Gitea repo\n" +
			"(cube-pack-<name>) instead of an OCI artifact — requires the gitea pack.",
		RunE: func(c *cobra.Command, args []string) error {
			// P7 (decision 4): per-pack delivery flag. Validated up front so
			// a bogus value refuses before any prompt or file touch.
			if via != "oci" && via != "repo" {
				return diag.New(diag.CodeBadFlagValue,
					fmt.Sprintf("--via must be oci or repo, got %q", via),
					"run `cube-idp pack install <ref> --via repo` for Gitea delivery, or drop the flag for the OCI default")
			}
			refs := args
			if len(refs) == 0 {
				// gh doctrine (spec Decision 4): bare + non-interactive must
				// refuse fast and name the scriptable twin — never hang.
				if !packPromptsAllowed(c.InOrStdin(), c.OutOrStdout()) {
					return diag.New(diag.CodeConfirmRequired,
						"pack install needs pack refs in non-interactive mode",
						"pass refs: cube-idp pack install oci://ghcr.io/cube-idp/packs/<name>:<version>")
				}
				// Catalog load strictly AFTER the prompt gate: the
				// non-interactive refusal must stay instant and offline.
				catalog := loadPackCatalog(c.Context(), c.OutOrStdout())
				names, err := packMenuSelect(c.InOrStdin(), c.OutOrStdout(), catalogOptions(catalog))
				if err != nil {
					return err
				}
				for _, n := range names {
					if ref := catalogRef(catalog, n); ref != "" {
						refs = append(refs, ref)
					}
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
			if err := packInstallRefs(c.Context(), c.OutOrStdout(), file, refs, via); err != nil {
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
	c.Flags().StringVar(&via, "via", "oci", "delivery mode for the installed packs: oci (default) or repo (editable Gitea repo; needs the gitea pack)")
	return c
}

// runPackMenu is the TTY half of `pack install` (spec WP6): a filterable huh
// MultiSelect over the loaded pack catalog (P6: remote index or built-in
// fallback — the caller passes the options so this stays pure UI). Callers
// gate on ui.PromptsAllowed; this function assumes the terminal is
// available.
func runPackMenu(in io.Reader, out io.Writer, opts []huh.Option[string]) ([]string, error) {
	var names []string
	form := huh.NewForm(huh.NewGroup(
		huh.NewMultiSelect[string]().
			Title("Packs to install").
			Options(opts...).
			Filterable(true).
			Value(&names),
	)).WithInput(in).WithOutput(out).WithAccessible(os.Getenv("ACCESSIBLE") != "")
	if err := form.Run(); err != nil {
		return nil, err
	}
	return names, nil
}

// packInstallRefs appends the non-duplicate refs to file's spec.packs and
// writes it back through config.SaveValidated (init's writer shape,
// validate-by-round-trip through config.Load — the exact schema +
// cross-field checks `up` applies — via a temp file, so a rejected ref
// leaves cube.yaml untouched). via "repo" (P7) marks each written ref for
// Gitea delivery; "oci" writes no delivery key at all — byte-compatible
// with pre-P7 files. The gitea guarantee (CUBE-7304) rides the round-trip:
// --via repo on a gitea-less cube is refused with the file untouched.
func packInstallRefs(ctx context.Context, out io.Writer, file string, refs []string, via string) error {
	cube, err := cfgload.Load(ctx, file)
	if err != nil {
		return err
	}
	delivery := ""
	if via == "repo" {
		delivery = "repo"
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
		cube.Spec.Packs = append(cube.Spec.Packs, config.PackRef{Ref: ref, Delivery: delivery})
		existing[ref] = true
		added = append(added, ref)
	}
	if len(added) == 0 {
		fmt.Fprintln(out, "nothing to add — cube.yaml unchanged")
		return nil
	}
	if err := config.SaveValidated(file, cube); err != nil {
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
