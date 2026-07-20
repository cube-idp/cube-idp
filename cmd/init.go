package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	huh "charm.land/huh/v2"
	"github.com/spf13/cobra"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/yaml"

	"github.com/cube-idp/cube-idp/internal/config"
	"github.com/cube-idp/cube-idp/internal/diag"
	"github.com/cube-idp/cube-idp/internal/doctor"
	"github.com/cube-idp/cube-idp/internal/ui"
)

// cubeNameRe mirrors internal/config/schema.cue's metadata.name pattern —
// the wizard validates interactively against the same rule Load() enforces,
// so a name accepted by the wizard is never rejected later by `up`.
var cubeNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,30}$`)

// optionalPacks is the catalog the wizard's pack multi-select offers, derived
// from pack.go's packCatalog (one source for wizard and `pack install`). A
// pack ref (OCI or --local path) is matched to its catalog name by substring,
// the same convention withoutGiteaPack uses; a ref matching none of these is
// treated as non-optional and always kept.
var optionalPacks = packCatalogNames()

// gatewayPacks is the catalog --gateway-pack and the wizard's "Gateway pack"
// Select accept — the two shipped gateway implementations (R7a). An unknown
// value is a CUBE-0007 preflight error, the existing enum-flag pattern.
var gatewayPacks = []string{"traefik", "envoy-gateway"}

// publishedGatewayVersion is the packs-monorepo release line the published
// gateway refs pin. Both gateway packs release in lockstep; keep this in
// sync with what cube-idp/packs actually tags — the same discipline packCatalog
// follows for its built-in offline fallback versions.
const publishedGatewayVersion = "0.2.0"

// publishedGatewayRef derives the published oci ref for a gateway pack —
// init's published-mode twin of the --local path join, so the §5.7a
// coherence rule (ref follows the final chosen pack) holds in both modes.
func publishedGatewayRef(pack string) string {
	return "oci://ghcr.io/cube-idp/packs/" + pack + ":" + publishedGatewayVersion
}

// validateGatewayPackFlag rejects an unrecognized --gateway-pack value with
// the same CUBE-0007 code addOutputFlag/validateProgressFlag use for other
// enum flags.
func validateGatewayPackFlag(v string) error {
	for _, p := range gatewayPacks {
		if v == p {
			return nil
		}
	}
	return diag.New(diag.CodeBadFlagValue,
		fmt.Sprintf("unknown --gateway-pack value %q", v),
		"use one of: traefik, envoy-gateway")
}

func newInitCmd() *cobra.Command {
	var name string
	var local string
	var engineType string
	var gatewayPack string
	c := &cobra.Command{
		Use:   "init",
		Short: "Write the default cube.yaml (kind + flux + traefik + gitea + argocd, D9)",
		RunE: func(c *cobra.Command, _ []string) error {
			if _, err := os.Stat("cube.yaml"); err == nil {
				return diag.New(diag.CodeInitExists, "cube.yaml already exists — refusing to overwrite",
					"remove or rename the existing cube.yaml, or edit it directly and re-run `cube-idp up`")
			}
			if err := validateGatewayPackFlag(gatewayPack); err != nil {
				return err
			}

			// Wizard answers default to the D9 profile so a run that only
			// touches some fields still writes a coherent cube.yaml.
			wiz := initWizardResult{
				Provider:    "kind",
				GatewayHost: "cube-idp.localtest.me",
				GatewayPort: 8443,
				GatewayPack: gatewayPack,
				Packs:       append([]string(nil), optionalPacks...),
			}
			wizardRan := false
			if wizardApplicable(c) {
				// The wizard's pack multi-select offers the remote
				// catalog (built-in fallback when the index is unreachable).
				// Loaded only on the interactive path — flag-driven runs,
				// CI, and the e2e suite never touch the network here.
				wiz.Catalog = loadPackCatalog(c.Context(), c.OutOrStdout())
				if err := runInitWizard(c, &name, &engineType, &wiz); err != nil {
					return err
				}
				wizardRan = true
			}

			cube := config.Default(name)
			cube.Spec.Engine.Type = engineType
			// engine.type: argocd installs Argo CD itself (UI included), so
			// the argocd pack would trip CUBE-0005 (redundant pack).
			if engineType == "argocd" {
				cube.Spec.Packs = []config.PackRef{
					{Ref: "oci://ghcr.io/cube-idp/packs/gitea:0.2.0"},
				}
			}
			var localAbs string
			if local != "" {
				abs, err := filepath.Abs(local)
				if err != nil {
					return fmt.Errorf("resolving --local %q: %w", local, err)
				}
				localAbs = abs
				cube.Spec.Packs = []config.PackRef{
					{Ref: filepath.Join(abs, "packs", "gitea")},
				}
				if engineType != "argocd" {
					cube.Spec.Packs = append(cube.Spec.Packs, config.PackRef{Ref: filepath.Join(abs, "packs", "argocd")})
				}
			}
			// The wizard's provider/context/gateway/pack answers apply last —
			// only ever set on an interactive run, so every flag-driven test,
			// the e2e suite, and CI keep today's D9 default profile unchanged.
			if wizardRan {
				applyWizardToCube(cube, wiz)
			} else {
				cube.Spec.Gateway.Pack = gatewayPack
			}
			// Gateway coherence rule: the gateway.ref is ALWAYS derived
			// from the final chosen pack (flag or wizard), never from an
			// assignment made before the wizard ran — that ordering is
			// exactly the incoherence CUBE-0008 exists to catch (ref traefik,
			// pack envoy). Since the packs moved out of this repo (published-mode
			// closed): published mode derives the oci ref the same way, so
			// init always writes a gateway source that works from any cwd.
			if localAbs != "" {
				cube.Spec.Gateway.Ref = filepath.Join(localAbs, "packs", cube.Spec.Gateway.Pack)
			} else {
				cube.Spec.Gateway.Ref = publishedGatewayRef(cube.Spec.Gateway.Pack)
			}
			// engine-as-pack: --local must resolve the engine pack from the
			// checkout too, else the written cube.yaml dials the published
			// cube-engine-<type> ref and `up` fails air-gapped / pre-publish
			// (CUBE-4012). Published mode leaves engine.ref unset so
			// EngineSpec.PackRef() falls back to defaultEngineRefs — same as
			// today. Mirrors the gateway.ref derivation directly above.
			if localAbs != "" {
				cube.Spec.Engine.Ref = filepath.Join(localAbs, "packs", cube.Spec.Engine.PackName())
			}
			out, err := yaml.Marshal(cube)
			if err != nil {
				return err
			}
			if err := os.WriteFile("cube.yaml", out, 0o644); err != nil {
				return err
			}
			fmt.Fprintln(c.OutOrStdout(), "wrote cube.yaml — run `cube-idp up`")
			return nil
		},
	}
	c.Flags().StringVar(&name, "name", "dev", "cube name")
	c.Flags().StringVar(&local, "local", "", "use pack dirs from a local cube-idp/packs checkout (path containing packs/<name>) instead of published OCI refs")
	c.Flags().StringVar(&engineType, "engine", "flux", "gitops engine: flux | argocd")
	c.Flags().StringVar(&gatewayPack, "gateway-pack", "traefik", "gateway implementation pack: traefik | envoy-gateway")
	return c
}

// initWizardResult holds the multi-group wizard's non-scalar answers (name and
// engine stay in their existing flag-backed vars). It is applied to the cube
// after config.Default + engine/local resolution by applyWizardToCube.
type initWizardResult struct {
	Provider    string // "kind" | "existing"
	Context     string // kubeconfig context, when Provider == "existing"
	GatewayHost string
	GatewayPort int
	GatewayPack string         // "traefik" | "envoy-gateway" (R7a)
	Packs       []string       // selected optional-pack catalog names
	Catalog     []catalogEntry // the loaded pack catalog the multi-select offered (remote index or built-in fallback)
}

// applyWizardToCube overlays the wizard answers onto cube, keeping the written
// cube.yaml loadable by config.Load: an "existing" provider clears the
// kind-only ClusterSpec fields (config.Load rejects kubernetesVersion on an
// existing provider), and the pack list is narrowed to the selected optional
// packs. Pure and side-effect-free so it is unit-testable without a TTY.
func applyWizardToCube(cube *config.Cube, r initWizardResult) {
	if r.Provider == "existing" {
		cube.Spec.Cluster = config.ClusterSpec{Provider: "existing", Context: r.Context}
	} else {
		cube.Spec.Cluster.Provider = "kind"
	}
	if r.GatewayHost != "" {
		cube.Spec.Gateway.Host = r.GatewayHost
	}
	if r.GatewayPort != 0 {
		cube.Spec.Gateway.Port = r.GatewayPort
	}
	if r.GatewayPack != "" {
		cube.Spec.Gateway.Pack = r.GatewayPack
	}
	cube.Spec.Packs = filterSelectedPacks(cube.Spec.Packs, r.Packs)
	// A selected catalog pack OUTSIDE the built-in list was never in the
	// default profile, so keeping the user's choice means appending its
	// published ref (spec B3: the wizard discovers packs without a binary
	// release). Built-in names never append here — their presence in the
	// default list is engine/--local logic's decision (e.g. engine argocd
	// drops the argocd pack; resurrecting it would re-trip CUBE-0005).
	builtin := map[string]bool{}
	for _, n := range packCatalogNames() {
		builtin[n] = true
	}
	for _, name := range r.Packs {
		if builtin[name] || packsContainName(cube.Spec.Packs, name) {
			continue
		}
		if ref := catalogRef(r.Catalog, name); ref != "" {
			cube.Spec.Packs = append(cube.Spec.Packs, config.PackRef{Ref: ref})
		}
	}
}

// packsContainName reports whether any pack ref already matches name by the
// established substring convention (packCatalogName's rule).
func packsContainName(packs []config.PackRef, name string) bool {
	for _, p := range packs {
		if strings.Contains(p.Ref, name) {
			return true
		}
	}
	return false
}

// filterSelectedPacks keeps every pack whose catalog name (gitea/argocd, by
// substring) is in selected; a pack matching no catalog name is non-optional
// and always kept.
func filterSelectedPacks(packs []config.PackRef, selected []string) []config.PackRef {
	sel := map[string]bool{}
	for _, s := range selected {
		sel[s] = true
	}
	kept := make([]config.PackRef, 0, len(packs))
	for _, p := range packs {
		name := packCatalogName(p.Ref)
		if name == "" || sel[name] {
			kept = append(kept, p)
		}
	}
	return kept
}

// packCatalogName maps a pack ref to its optionalPacks catalog name, or "" if
// it is not an optional pack.
func packCatalogName(ref string) string {
	for _, n := range optionalPacks {
		if strings.Contains(ref, n) {
			return n
		}
	}
	return ""
}

// withoutGiteaPack drops the gitea pack ref (OCI or --local path — both
// contain "gitea" as their pack directory/image name) from packs. Retained for
// the pre-14c call sites and tests; filterSelectedPacks generalizes it.
func withoutGiteaPack(packs []config.PackRef) []config.PackRef {
	return filterSelectedPacks(packs, []string{"argocd"})
}

// wizardApplicable reports whether it is safe and appropriate to prompt
// interactively: both stdin and stdout must be real terminals (never true
// in CI, in `go test`, or when init's output is piped — the e2e suite pipes
// this command, so it must never block), and none of --name/--engine/
// --gateway-pack was explicitly passed (flags always win over the wizard).
func wizardApplicable(c *cobra.Command) bool {
	if c.Flags().Changed("name") || c.Flags().Changed("engine") || c.Flags().Changed("gateway-pack") {
		return false
	}
	return ui.IsTerminal(c.InOrStdin()) && ui.IsTerminal(c.OutOrStdout())
}

// kubeContextNames returns the sorted kubeconfig context names (honoring
// $KUBECONFIG) for the wizard's existing-provider picker, or nil when the
// kubeconfig is missing/unreadable — the wizard then falls back to a free-text
// context entry.
func kubeContextNames() []string {
	cfg, err := clientcmd.NewDefaultClientConfigLoadingRules().Load()
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(cfg.Contexts))
	for n := range cfg.Contexts {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// runInitWizard runs the multi-group huh v2 wizard: cube name
// (validated by cubeNameRe, the schema.cue mirror), provider (kind | existing,
// with a kubeconfig context picker for existing), engine (flux | argocd),
// gateway host/port with a port-conflict pre-check via doctor.CheckPortFree, a
// gateway pack Select (traefik | envoy-gateway, R7a), and an optional-pack
// multi-select. Answers are written back into *name/*engineType and *res.
// Accessible (screen-reader) mode engages when $ACCESSIBLE is set — the gh
// screen-reader-prompter precedent.
func runInitWizard(c *cobra.Command, name, engineType *string, res *initWizardResult) error {
	accessible := os.Getenv("ACCESSIBLE") != ""
	portStr := strconv.Itoa(res.GatewayPort)

	main := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Cube name").
				Value(name).
				Validate(func(v string) error {
					if !cubeNameRe.MatchString(v) {
						return fmt.Errorf("must match %s", cubeNameRe.String())
					}
					return nil
				}),
			huh.NewSelect[string]().
				Title("Cluster provider").
				Options(
					huh.NewOption("kind (create a local cluster)", "kind"),
					huh.NewOption("existing (use a kubeconfig context)", "existing"),
				).
				Value(&res.Provider),
			huh.NewSelect[string]().
				Title("GitOps engine").
				Options(
					huh.NewOption("flux", "flux"),
					huh.NewOption("argocd", "argocd"),
				).
				Value(engineType),
		),
		huh.NewGroup(
			huh.NewInput().
				Title("Gateway host").
				Value(&res.GatewayHost),
			huh.NewInput().
				Title("Gateway port").
				Value(&portStr).
				Validate(validateGatewayPort),
			huh.NewSelect[string]().
				Title("Gateway pack").
				Options(
					huh.NewOption("traefik", "traefik"),
					huh.NewOption("envoy-gateway", "envoy-gateway"),
				).
				Value(&res.GatewayPack),
			huh.NewMultiSelect[string]().
				Title("Optional packs").
				Options(catalogOptions(res.Catalog)...).
				Value(&res.Packs),
		),
	).WithOutput(c.OutOrStdout()).WithInput(c.InOrStdin()).WithAccessible(accessible)
	if err := main.Run(); err != nil {
		return err
	}
	res.GatewayPort, _ = strconv.Atoi(portStr) // already validated as a port

	// Second form: pick the kubeconfig context only when the user chose the
	// existing provider (huh v2 in this build has no field-level hide, so a
	// conditional follow-up form is cleaner than a hidden group). With no
	// contexts discoverable, fall back to free-text entry.
	if res.Provider == "existing" {
		if ctxs := kubeContextNames(); len(ctxs) > 0 {
			if res.Context == "" {
				res.Context = ctxs[0]
			}
			opts := make([]huh.Option[string], 0, len(ctxs))
			for _, n := range ctxs {
				opts = append(opts, huh.NewOption(n, n))
			}
			pick := huh.NewForm(huh.NewGroup(
				huh.NewSelect[string]().Title("Kubeconfig context").Options(opts...).Value(&res.Context),
			)).WithOutput(c.OutOrStdout()).WithInput(c.InOrStdin()).WithAccessible(accessible)
			return pick.Run()
		}
		entry := huh.NewForm(huh.NewGroup(
			huh.NewInput().Title("Kubeconfig context").Value(&res.Context).
				Validate(func(v string) error {
					if strings.TrimSpace(v) == "" {
						return fmt.Errorf("a context name is required for the existing provider")
					}
					return nil
				}),
		)).WithOutput(c.OutOrStdout()).WithInput(c.InOrStdin()).WithAccessible(accessible)
		return entry.Run()
	}
	return nil
}

// validateGatewayPort parses a gateway port and runs doctor's CheckPortFree
// pre-check: a syntactically bad port or one already bound by
// a non-cube process (an error-severity finding) is rejected inline so the
// wizard never writes a cube.yaml whose `up` would immediately fail the same
// port check. clusterExists=false: init runs before any cube exists.
func validateGatewayPort(v string) error {
	port, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("must be a port number 1-65535")
	}
	if f := doctor.CheckPortFree(port, false); f != nil && f.Severity == diag.SeverityError {
		return fmt.Errorf("%s", f.Message)
	}
	return nil
}
