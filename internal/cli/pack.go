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
	cmd.AddCommand(newPackRenderCmd(), newPackValidateCmd())
	return cmd
}

func newPackRenderCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "render <ref>",
		Short: "Render a pack to Kubernetes YAML on stdout",
		Long: "Render a pack to Kubernetes YAML on stdout.\n\n" +
			"Only rendered YAML reaches stdout — diagnostics go to stderr — so the " +
			"output pipes straight into `kubectl apply -f -`. Nothing is written when " +
			"rendering fails.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := loadPack(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			plan, err := p.Render(cmd.Context(), pack.RenderOptions{})
			if err != nil {
				return err
			}
			return writePlan(cmd.OutOrStdout(), plan)
		},
	}
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
