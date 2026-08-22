package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	v1alpha1 "github.com/cube-idp/cube-idp/api/config/v1alpha1"
	"github.com/cube-idp/cube-idp/internal/config"
	"github.com/cube-idp/cube-idp/internal/pack"
)

// renderTarget decides which of render's two forms was asked for and produces
// its plan.
//
// The forms are exclusive on purpose. A reference renders the pack as its
// author wrote it; -f with --id renders one instance as the setup configures
// it — different inputs and different output, so silently preferring one would
// leave a user guessing which they got. -f carries a default, so only an
// explicitly given one counts as asking for instance mode.
func renderTarget(cmd *cobra.Command, args []string) (pack.RenderPlan, error) {
	id, _ := cmd.Flags().GetString("id")
	path, _ := cmd.Flags().GetString("config")
	configured := cmd.Flags().Changed("config")

	if len(args) == 1 {
		if id != "" || configured {
			return pack.RenderPlan{}, fmt.Errorf(
				"render takes either a pack reference or -f with --id, not both: drop %q to render the configured instance, or drop the flags to render the pack as authored", args[0])
		}
		return renderArtifact(cmd.Context(), args[0])
	}

	switch {
	case id == "" && !configured:
		return pack.RenderPlan{}, fmt.Errorf(
			"render needs a pack reference (`pack render ./dir`) or an instance to render (`pack render -f %s --id <id>`)", path)
	case id == "":
		return pack.RenderPlan{}, fmt.Errorf(
			"-f %s selects a Config document but not what to render from it: add --id <id>", path)
	case !configured:
		return pack.RenderPlan{}, fmt.Errorf(
			"--id %s renders an instance of a Config document: add -f <path> to say which document", id)
	}
	return renderConfigured(cmd.Context(), path, id)
}

// renderArtifact renders a pack as authored, with no setup around it.
func renderArtifact(ctx context.Context, ref string) (pack.RenderPlan, error) {
	p, err := loadPack(ctx, ref)
	if err != nil {
		return pack.RenderPlan{}, err
	}
	return p.Render(ctx, pack.RenderOptions{})
}

// renderConfigured renders the instance the Config document calls id: its
// values, its external manifests, and the namespace its pack forces.
//
// Unlike artifact mode this reads the real sources the setup points at — the
// pack, its valuesRef, its external refs. `config validate` stays local-only;
// rendering an instance cannot, because an instance is only defined by what
// its references resolve to.
func renderConfigured(ctx context.Context, path, id string) (pack.RenderPlan, error) {
	cfg, err := config.LoadFile(path)
	if err != nil {
		return pack.RenderPlan{}, err
	}

	instances, packs, err := loadInstances(ctx, cfg.Spec.Packs)
	if err != nil {
		return pack.RenderPlan{}, err
	}
	i, err := selectInstance(instances, id, path)
	if err != nil {
		return pack.RenderPlan{}, err
	}
	return pack.RenderInstance(ctx, packs[i], cfg.Spec.Packs[i])
}

// loadInstances loads every pack in the setup, index-aligned with specs.
//
// Every one, not just the requested instance: an effective ID is a property of
// the whole setup — a pack's own name serves as its ID only while no other
// entry shares it — so the identity of one entry cannot be decided without
// reading the others' packs.
func loadInstances(ctx context.Context, specs []v1alpha1.PackSpec) ([]pack.Instance, []*pack.Pack, error) {
	instances := make([]pack.Instance, 0, len(specs))
	packs := make([]*pack.Pack, 0, len(specs))

	for _, spec := range specs {
		p, err := loadPack(ctx, spec.PackRef)
		if err != nil {
			return nil, nil, err
		}
		instances = append(instances, pack.Instance{Name: p.Metadata().Name, Spec: spec})
		packs = append(packs, p)
	}
	return instances, packs, nil
}

// selectInstance finds the entry whose effective ID is id.
//
// Deriving the IDs is the domain's job, so a mistyped --id is reported against
// the same identities delivery will use — and the message lists them, because
// an ID a user never wrote (a defaulted one) is exactly the ID they are most
// likely to get wrong.
func selectInstance(instances []pack.Instance, id, path string) (int, error) {
	ids, err := pack.EffectiveIDs(instances)
	if err != nil {
		return 0, err
	}
	for i, got := range ids {
		if string(got) == id {
			return i, nil
		}
	}

	names := make([]string, 0, len(ids))
	for _, got := range ids {
		names = append(names, string(got))
	}
	if len(names) == 0 {
		return 0, fmt.Errorf("%s declares no packs, so there is no instance %q to render", path, id)
	}
	return 0, fmt.Errorf("%s has no pack instance %q; it declares %s", path, id, strings.Join(names, ", "))
}
