package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"

	"github.com/cube-idp/cube-idp/internal/cubeerr"
	"github.com/cube-idp/cube-idp/internal/pack"
)

// documentSeparator goes between rendered documents, never before the first:
// the output is a valid multi-document stream that `kubectl apply -f -`
// accepts, byte-for-byte reproducible across runs.
const documentSeparator = "---\n"

func newPackCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pack",
		Short: "Define, validate, and render packs",
	}
	cmd.AddCommand(newPackRenderCmd(), newPackValidateCmd(), newPackNewCmd())
	return cmd
}

func newPackNewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "new <dir>",
		Short: "Create a new pack that renders as written",
		Long: "Create a new pack in a directory that does not exist yet.\n\n" +
			"The result is a complete pack — pack.cue plus a payload matching its type — " +
			"so `cube-idp pack render <dir>` works on it immediately. With --from, an " +
			"existing pack is copied instead of scaffolded, and keeps the type it declares. " +
			"With --from-chart, a local Helm chart directory is read (Chart.yaml and " +
			"values.yaml only, never fetched or copied) and a thin helm pack is scaffolded " +
			"from it, with a placeholder repository URL to replace.",
		Args: cobra.ExactArgs(1),
		RunE: runPackNew,
	}
	cmd.Flags().String("type", string(pack.TypeRaw), "pack type to scaffold: raw, helm, or kustomize")
	cmd.Flags().String("name", "", "pack name (default: the directory's base name)")
	cmd.Flags().String("from", "", "reference to an existing pack to fork instead of scaffolding")
	cmd.Flags().String("from-chart", "", "local Helm chart directory to scaffold a thin helm pack from")
	return cmd
}

// runPackNew maps the flags and reports what was created. The pack is loaded
// back afterwards: it is the cheapest possible check that the command made
// something this tool can actually read, and it is what supplies the type for
// a fork, which the flags never named.
func runPackNew(cmd *cobra.Command, args []string) error {
	opts, err := packNewOptions(cmd, args[0])
	if err != nil {
		return err
	}
	if err := pack.New(cmd.Context(), opts); err != nil {
		return err
	}

	p, err := loadPack(cmd.Context(), args[0])
	if err != nil {
		return err
	}
	meta := p.Metadata()
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "created pack %s %s (%s) in %s\n",
		meta.Name, meta.Version, meta.Type, args[0])
	if opts.FromChart != "" {
		// A local chart directory does not say where the chart is published,
		// so the scaffold carries a placeholder URL. Saying so here as well as
		// in the file is the difference between a TODO someone reads and one
		// they discover from a failing HelmRelease.
		_, _ = fmt.Fprintf(cmd.OutOrStdout(),
			"replace the placeholder chart url in %s before installing this pack\n",
			filepath.Join(args[0], "pack.cue"))
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "run \"cube-idp pack render %s\" to see what it produces\n", args[0])
	return nil
}

// packNewOptions maps the flags, refusing the combinations that cannot mean
// anything: a fork takes the type its source declares and a chart scaffolds a
// helm pack, so asking for a different --type is not a conversion this command
// could perform, and asking for both sources names two different operations on
// two different kinds of input. --type carries a default, so only an
// explicitly given one is a request.
func packNewOptions(cmd *cobra.Command, dir string) (pack.NewOptions, error) {
	name, _ := cmd.Flags().GetString("name")
	from, _ := cmd.Flags().GetString("from")
	fromChart, _ := cmd.Flags().GetString("from-chart")
	packType, _ := cmd.Flags().GetString("type")

	switch {
	case from != "" && fromChart != "":
		return pack.NewOptions{}, fmt.Errorf(
			"--from forks an existing pack and --from-chart scaffolds one from a Helm chart: give exactly one")
	case from != "" && cmd.Flags().Changed("type"):
		return pack.NewOptions{}, fmt.Errorf(
			"--from copies a pack with the type it already declares, so --type %s cannot apply: drop --type to fork, or drop --from to scaffold a new %s pack",
			packType, packType)
	case fromChart != "" && cmd.Flags().Changed("type"):
		return pack.NewOptions{}, fmt.Errorf(
			"--from-chart always scaffolds a helm pack, so --type %s cannot apply: drop --type, or drop --from-chart to scaffold a %s pack",
			packType, packType)
	case from != "":
		return pack.NewOptions{Dir: dir, Name: name, From: from}, nil
	case fromChart != "":
		return pack.NewOptions{Dir: dir, Name: name, FromChart: fromChart}, nil
	}
	return pack.NewOptions{Dir: dir, Name: name, Type: pack.Type(packType)}, nil
}

func newPackRenderCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "render [<ref>]",
		Short: "Render a pack, or a configured pack instance, to Kubernetes YAML on stdout",
		Long: "Render a pack to Kubernetes YAML on stdout.\n\n" +
			"`pack render <ref>` renders a pack as its author wrote it. " +
			"`pack render -f <config> --id <id>` renders one instance of that pack as a " +
			"Config document configures it: its values, its external manifests, and the " +
			"namespace its pack forces. The two forms are mutually exclusive.\n\n" +
			"Only rendered YAML reaches stdout — diagnostics go to stderr — so the " +
			"output pipes straight into `kubectl apply -f -`. Nothing is written when " +
			"rendering fails.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			plan, err := renderTarget(cmd, args)
			if err != nil {
				return err
			}
			return writePlan(cmd.OutOrStdout(), plan)
		},
	}
	cmd.Flags().String("id", "",
		"render this pack instance from the Config document given with -f")
	return cmd
}

func newPackValidateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate <ref>",
		Short: "Load a pack and validate its metadata and payload",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := loadPack(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if err := validateRenders(cmd.Context(), p); err != nil {
				return err
			}
			meta := p.Metadata()
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "pack %s %s (%s) is valid\n",
				meta.Name, meta.Version, meta.Type)
			return nil
		},
	}
}

// validateRenders renders the pack and discards the output, so validate
// reports everything render would hit — an unparseable manifest, an empty
// result, a namespace conflict, values the pack rejects. Without this,
// validate could call a pack valid that render then refuses.
//
// A type this build cannot render still validates: its metadata and payload
// are sound and only the render backend is missing, which is not the pack's
// problem to fix.
func validateRenders(ctx context.Context, p *pack.Pack) error {
	_, err := p.Render(ctx, pack.RenderOptions{})

	var coded *cubeerr.Coded
	if errors.As(err, &coded) && coded.Code == pack.CodeRenderTypeUnsupported {
		return nil
	}
	return err
}

// loadPack resolves a pack reference and loads the pack behind it.
//
// This build resolves only the local-path form of the reference grammar —
// ./dir, ../dir, /abs/dir, a bare relative path, or file:///abs/dir. The
// remote schemes (git+https, oci, s3) are the reference leaf's job; when that
// lands it replaces this function's implementation, not the <ref> contract
// the command already exposes.
func loadPack(ctx context.Context, ref string) (*pack.Pack, error) {
	dir, err := localRefDir(ref)
	if err != nil {
		return nil, err
	}
	return pack.Load(ctx, os.DirFS(dir), ref)
}

// localRefDir turns a local-path reference into a directory path. A remote
// scheme is rejected here rather than silently treated as a relative path,
// so the error names the real limitation.
func localRefDir(ref string) (string, error) {
	if after, ok := strings.CutPrefix(ref, "file://"); ok {
		return filepath.FromSlash(after), nil
	}
	if scheme, _, ok := strings.Cut(ref, "://"); ok {
		return "", pack.NewRefUnsupportedError(ref, scheme)
	}
	return filepath.FromSlash(ref), nil
}

// writePlan renders the plan into memory and writes it in one call, so a
// failure part-way through leaves stdout untouched. Prerequisites precede the
// pack's own objects: one deterministic stream, with the group boundary
// carried by RenderPlan rather than encoded in the YAML.
func writePlan(w io.Writer, plan pack.RenderPlan) error {
	var buf bytes.Buffer
	objs := make([]map[string]any, 0, len(plan.Prerequisites)+len(plan.Objects))
	for _, obj := range plan.Prerequisites {
		objs = append(objs, obj.Object)
	}
	for _, obj := range plan.Objects {
		objs = append(objs, obj.Object)
	}

	for i, obj := range objs {
		if i > 0 {
			buf.WriteString(documentSeparator)
		}
		out, err := yaml.Marshal(obj)
		if err != nil {
			return fmt.Errorf("encode rendered object %d as YAML: %w", i, err)
		}
		buf.Write(out)
	}

	_, err := w.Write(buf.Bytes())
	return err
}
