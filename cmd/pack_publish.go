// pack_publish.go is the packs-repo CI toolchain: `pack
// publish` (validate + push one pack, print its digest), `pack index build`
// (assemble the catalog index artifact payload over a packs/ tree), and
// `pack index push` (ship index.json as an OCI artifact). The publish
// workflow in cube-idp/packs (.github/workflows/publish.yml) is the
// production caller; everything here is also runnable locally against any
// registry the docker credential chain can reach.
//
// Digest sourcing for `index build`:
// internal/oci exports no offline digest helper (pushPackDirTo is an
// unexported seam), so per the plan's fallback the build takes repeatable
// `--digest name=sha256:…` flags (CI passes the digests `pack publish` just
// printed) and `--from-registry` (pack.ResolveRemote HEADs the registry —
// resolve without pull) for packs not republished in the current run.
package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cube-idp/cube-idp/internal/diag"
	"github.com/cube-idp/cube-idp/internal/oci"
	"github.com/cube-idp/cube-idp/internal/pack"
	"github.com/cube-idp/cube-idp/internal/ui"
)

// packIndex is the catalog index artifact schema (schemaVersion 1),
// consumed by the publish CI's build attestation and by the remote catalog
// surfaces in this package. Additive-only
// within schemaVersion 1.
type packIndex struct {
	SchemaVersion int              `json:"schemaVersion"`
	Packs         []packIndexEntry `json:"packs"`
}

type packIndexEntry struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
	Ref         string `json:"ref"`
	Digest      string `json:"digest"`
}

func newPackPublishCmd() *cobra.Command {
	var ref string
	c := &cobra.Command{
		Use:   "publish <dir>",
		Short: "Validate a pack directory and publish it as an OCI artifact",
		Long: "Publish one pack source directory to an OCI registry — the packs-repo CI\n" +
			"entrypoint. Unlike `pack push` it enforces the release invariant first:\n" +
			"the ref's tag must equal pack.cue's version (a missing tag defaults to it).\n" +
			"Prints the pushed manifest digest; auth is the ambient docker credential\n" +
			"chain (docker login).",
		Args: cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			return ui.RunPipelineStatic(c.Context(), "pack", c.OutOrStdout(),
				func(ctx context.Context, con *ui.Console) error {
					con.Start("pack", "")
					dir := args[0]
					p, err := pack.Fetch(ctx, dir, "")
					if err != nil {
						return err
					}
					target := ref
					if !refHasTag(target) {
						target += ":" + p.Version
					}
					if strings.HasPrefix(target, "oci://") {
						if tag := ociRefTag(target); tag != p.Version {
							return diag.New(diag.CodePackRefInvalid,
								fmt.Sprintf("pack %s version %s does not match the publish tag %q", p.Name, p.Version, tag),
								"the publish tag must equal pack.cue's version (git tag <name>/vX.Y.Z publishes :X.Y.Z) — retag the ref or bump the pack version")
						}
					}
					digest, err := oci.PushPackDir(ctx, dir, target)
					if err != nil {
						return err
					}
					con.Step("pack", "published %s@%s", target, digest)
					return nil
				})
		},
	}
	c.Flags().StringVar(&ref, "ref", "", "target OCI ref (oci://host/repo[:tag]; the tag defaults to the pack's version)")
	_ = c.MarkFlagRequired("ref")
	return c
}

func newPackIndexCmd() *cobra.Command {
	c := &cobra.Command{Use: "index", Short: "Build and push the pack catalog index artifact"}
	c.AddCommand(newPackIndexBuildCmd(), newPackIndexPushCmd())
	return c
}

func newPackIndexBuildCmd() *cobra.Command {
	var outPath, refBase string
	var digestFlags []string
	var fromRegistry bool
	c := &cobra.Command{
		Use:   "build <packs-dir>",
		Short: "Build index.json over every pack directory in <packs-dir>",
		Long: "Assemble the schemaVersion-1 catalog index over a packs/ tree: one entry\n" +
			"per pack directory with name, version, description (contract v1 requires\n" +
			"it), ref (--ref-base + name:version), and digest. Digests come from\n" +
			"repeatable --digest flags (as printed by `pack publish`) and/or\n" +
			"--from-registry, which resolves the rest by HEADing the registry.",
		Args: cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			return ui.RunPipelineStatic(c.Context(), "pack", c.OutOrStdout(),
				func(ctx context.Context, con *ui.Console) error {
					con.Start("pack", "")
					if !strings.HasPrefix(refBase, "oci://") {
						return diag.New(diag.CodePackRefInvalid,
							fmt.Sprintf("--ref-base %q is not an oci:// reference", refBase),
							"use the form oci://host/repo (e.g. oci://ghcr.io/cube-idp/packs)")
					}
					digests := make(map[string]string, len(digestFlags))
					for _, d := range digestFlags {
						name, dg, ok := strings.Cut(d, "=")
						if !ok || name == "" || !strings.HasPrefix(dg, "sha256:") {
							return diag.New(diag.CodePackRefInvalid,
								fmt.Sprintf("--digest %q is not of the form name=sha256:…", d),
								"pass --digest <pack>=<sha256:…> exactly as `cube-idp pack publish` printed it")
						}
						digests[name] = dg
					}
					entries, err := buildPackIndex(ctx, args[0], refBase, digests, fromRegistry)
					if err != nil {
						return err
					}
					raw, err := json.MarshalIndent(packIndex{SchemaVersion: 1, Packs: entries}, "", "  ")
					if err != nil {
						return err
					}
					if err := os.WriteFile(outPath, append(raw, '\n'), 0o644); err != nil {
						return diag.Wrap(err, diag.CodePackManifestErr,
							fmt.Sprintf("cannot write index file %s", outPath),
							"check the output path is writable")
					}
					con.Step("pack", "index %s written (%d packs)", outPath, len(entries))
					return nil
				})
		},
	}
	c.Flags().StringVarP(&outPath, "output", "o", "index.json", "output path for the index JSON")
	c.Flags().StringVar(&refBase, "ref-base", "oci://ghcr.io/cube-idp/packs", "OCI ref prefix pack artifacts live under")
	c.Flags().StringArrayVar(&digestFlags, "digest", nil, "pack digest as <name>=<sha256:…> (repeatable; from `pack publish` output)")
	c.Flags().BoolVar(&fromRegistry, "from-registry", false, "resolve digests missing from --digest by HEADing the registry (no pull)")
	return c
}

// buildPackIndex walks packsDir (one pack per subdirectory), enforcing the
// contract-v1 fields the index publishes (description present, pack.cue
// name == directory name) and resolving each pack's digest from the
// digests map or — with fromRegistry — from the registry via
// pack.ResolveRemote. Entries come back sorted by name so the index is
// byte-deterministic for identical inputs (the artifact republish no-op
// property — see docs/adr/0035-reproducible-digest-pinned-artifacts.md).
func buildPackIndex(ctx context.Context, packsDir, refBase string, digests map[string]string, fromRegistry bool) ([]packIndexEntry, error) {
	dirEntries, err := os.ReadDir(packsDir)
	if err != nil {
		return nil, diag.Wrap(err, diag.CodePackManifestErr,
			fmt.Sprintf("cannot read packs directory %s", packsDir),
			"point `pack index build` at the packs/ tree (one pack per subdirectory)")
	}
	var entries []packIndexEntry
	for _, e := range dirEntries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(packsDir, e.Name())
		p, err := pack.Fetch(ctx, dir, "")
		if err != nil {
			return nil, err
		}
		if p.Name != e.Name() {
			return nil, diag.New(diag.CodePackCueInvalid,
				fmt.Sprintf("pack directory %q declares name %q", e.Name(), p.Name),
				"contract v1: pack.cue's name must equal the directory (and artifact) name")
		}
		if p.Description == "" {
			return nil, diag.New(diag.CodePackCueInvalid,
				fmt.Sprintf("pack %q has no description", p.Name),
				"contract v1 requires a one-line description in pack.cue — add description: \"…\"")
		}
		ref := strings.TrimSuffix(refBase, "/") + "/" + p.Name + ":" + p.Version
		dg, ok := digests[p.Name]
		if !ok {
			if !fromRegistry {
				return nil, diag.New(diag.CodePackRefInvalid,
					fmt.Sprintf("no digest for pack %q", p.Name),
					"pass --digest "+p.Name+"=sha256:… (from `pack publish` output) or --from-registry to resolve it from the registry")
			}
			pin, err := pack.ResolveRemote(ctx, ref, "")
			if err != nil {
				return nil, err
			}
			dg = strings.TrimPrefix(pin, "oci:")
		}
		entries = append(entries, packIndexEntry{
			Name:        p.Name,
			Version:     p.Version,
			Description: p.Description,
			Ref:         ref,
			Digest:      dg,
		})
	}
	if len(entries) == 0 {
		return nil, diag.New(diag.CodePackManifestErr,
			fmt.Sprintf("no pack directories found in %s", packsDir),
			"an empty index would wipe the published catalog — point at the packs/ tree (one pack per subdirectory)")
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return entries, nil
}

func newPackIndexPushCmd() *cobra.Command {
	var ref string
	c := &cobra.Command{
		Use:   "push <index.json>",
		Short: "Push a built index.json as the catalog index OCI artifact",
		Long: "Ship the `pack index build` output as an OCI artifact (one-file directory\n" +
			"artifact, same shape as a pack) and print its digest. The file is checked\n" +
			"to be a schemaVersion-1 index before anything touches the network.",
		Args: cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			return ui.RunPipelineStatic(c.Context(), "pack", c.OutOrStdout(),
				func(ctx context.Context, con *ui.Console) error {
					con.Start("pack", "")
					raw, err := os.ReadFile(args[0])
					if err != nil {
						return diag.Wrap(err, diag.CodePackManifestErr,
							fmt.Sprintf("cannot read index file %s", args[0]),
							"run `cube-idp pack index build` first")
					}
					var idx packIndex
					if err := json.Unmarshal(raw, &idx); err != nil {
						return diag.Wrap(err, diag.CodePackManifestErr,
							fmt.Sprintf("index file %s is not valid JSON", args[0]),
							"rebuild it with `cube-idp pack index build`")
					}
					if idx.SchemaVersion != 1 {
						return diag.New(diag.CodePackManifestErr,
							fmt.Sprintf("index file %s has schemaVersion %d, want 1", args[0], idx.SchemaVersion),
							"rebuild it with `cube-idp pack index build`")
					}
					tmp, err := os.MkdirTemp("", "cube-idp-index-*")
					if err != nil {
						return err
					}
					defer os.RemoveAll(tmp)
					if err := os.WriteFile(filepath.Join(tmp, "index.json"), raw, 0o644); err != nil {
						return err
					}
					digest, err := oci.PushPackDir(ctx, tmp, ref)
					if err != nil {
						return err
					}
					con.Step("pack", "pushed %s@%s", ref, digest)
					return nil
				})
		},
	}
	c.Flags().StringVar(&ref, "ref", "", "target OCI ref (oci://host/repo:tag, e.g. oci://ghcr.io/cube-idp/packs/index:latest)")
	_ = c.MarkFlagRequired("ref")
	return c
}

// ociRefTag returns the :tag of an oci:// ref ("" when none). A @digest ref
// returns "" — publish requires a version TAG (the release convention is that
// the artifact tag equals the pack's declared version), a
// digest-only target cannot satisfy that. Companion of refHasTag: same
// last-segment rule, so a port in the host is never mistaken for a tag.
func ociRefTag(ref string) string {
	rest, ok := strings.CutPrefix(ref, "oci://")
	if !ok {
		return ""
	}
	last := rest
	if i := strings.LastIndexByte(rest, '/'); i != -1 {
		last = rest[i+1:]
	}
	if strings.ContainsRune(last, '@') {
		return ""
	}
	if i := strings.LastIndexByte(last, ':'); i != -1 {
		return last[i+1:]
	}
	return ""
}
