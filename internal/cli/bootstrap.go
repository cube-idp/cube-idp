package cli

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	v1alpha1 "github.com/cube-idp/cube-idp/api/config/v1alpha1"
	"github.com/cube-idp/cube-idp/internal/bootstrap"
	"github.com/cube-idp/cube-idp/internal/ca"
	"github.com/cube-idp/cube-idp/internal/cluster"
	"github.com/cube-idp/cube-idp/internal/config"
	"github.com/cube-idp/cube-idp/internal/engine"
	"github.com/cube-idp/cube-idp/internal/engine/flux"
	"github.com/cube-idp/cube-idp/internal/engine/substrate"
	"github.com/cube-idp/cube-idp/internal/kube"
)

// engineFactory maps a validated engine provider to its tier-2 driver.
// Same doctrine as provisionerFactory: driver selection is edge
// composition, injected as a parameter so tests pass a seam instead of
// mutating package state.
type engineFactory func(p v1alpha1.EngineProvider) (engine.Provider, error)

// defaultEngine is the production engine factory: flux is the only
// driver today — adding one is a design-gate event. An unknown provider
// is the defensive CUBE-ENG-001; config validation is the primary gate.
func defaultEngine(p v1alpha1.EngineProvider) (engine.Provider, error) {
	switch p {
	case v1alpha1.EngineProviderFlux:
		return flux.New(), nil
	default:
		return nil, engine.NewUnsupportedProviderError(string(p))
	}
}

// bootstrapDeps are the bootstrap verb's injected seams: the clock and the
// entropy the CA mint is a function of, and the home lookup the operator
// artifact path resolves from. Injected for trustDeps' reason — a test passes
// a value instead of mutating package state, and nothing in a test reaches
// the real clock or the real $HOME.
type bootstrapDeps struct {
	now     func() time.Time
	rand    io.Reader
	homeDir func() (string, error)
}

// defaultBootstrapDeps is the production composition of the bootstrap verb.
func defaultBootstrapDeps() bootstrapDeps {
	return bootstrapDeps{now: time.Now, rand: rand.Reader, homeDir: os.UserHomeDir}
}

// defaultBootstrapTimeout bounds the whole install as one budget: the
// substrate's kind-set wait, every prerequisite unit's wait, the sync wiring
// and the engine reconciliation.
//
// It is 10 minutes rather than the 5 an engine-only bootstrap needed because
// M11 makes the run network-dependent inside the cluster: the helm-controller
// pulls the pinned gateway chart from its OCI registry during the gateway
// unit's wait, on a cluster still warming its images.
const defaultBootstrapTimeout = 10 * time.Minute

// edgeIOTimeout bounds each of the edge's own two cluster round-trips — the
// CA Secret read and the CoreDNS read-modify-write. They are not bootstrap
// phases, so they are deliberately not taken from --timeout, which is the
// readiness budget: a round-trip inheriting a nearly-spent budget would turn
// a successful bootstrap into a failed one at its last step.
const edgeIOTimeout = 30 * time.Second

func newBootstrapCmd(newProvisioner provisionerFactory, newEngine engineFactory, deps bootstrapDeps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bootstrap",
		Short: "Install the gitops engine (Flux) into the cluster and wait for it to be ready",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runBootstrap(cmd, newProvisioner, newEngine, deps)
		},
	}
	cmd.Flags().String("kubeconfig", "",
		"use this kubeconfig file instead of the default location")
	cmd.Flags().String("kubeconfig-context-name", "",
		"override the context name to use (default: cube-idp.dev/<cluster-name>)")
	cmd.Flags().Duration("timeout", defaultBootstrapTimeout,
		"how long to wait for the bootstrap resources to become ready")
	return cmd
}

// bootstrapOutcome is what one run composed and established, carried from
// composition through the edge's post-install steps into the success report.
type bootstrapOutcome struct {
	cfg     *v1alpha1.Config
	domain  string
	ensured ca.EnsureResult
}

// runBootstrap composes the bootstrap at the CLI edge: it asserts the
// requested engine version against the substrate pin, builds the kube clients
// (construction confined to internal/kube) and injects them into a bootstrap
// Applier, ensures the cube's CA material, resolves spec.prerequisites into
// the ordered units, and hands bootstrap the substrate's install objects, the
// units, the engine driver's sync wiring and its reconciliation judgment.
// bootstrap stays kube-free and engine-free — the edge injects content and
// judgment alike, and performs the two cluster round-trips that are nobody's
// domain operation.
func runBootstrap(cmd *cobra.Command, newProvisioner provisionerFactory, newEngine engineFactory, deps bootstrapDeps) error {
	path, _ := cmd.Flags().GetString("config")
	kubeconfigPath, _ := cmd.Flags().GetString("kubeconfig")
	contextName, _ := cmd.Flags().GetString("kubeconfig-context-name")
	timeout, _ := cmd.Flags().GetDuration("timeout")

	cfg, err := config.LoadFile(path)
	if err != nil {
		return err
	}
	if cfg.Spec.Cluster == nil {
		return cluster.NewNoClusterConfiguredError()
	}
	if err := substrate.CheckVersion(engineVersion(cfg.Spec.Engine)); err != nil {
		return err
	}
	// The engine's content is composed before the cluster is reached: a
	// driver failure is cheap and stays ahead of the first I/O.
	wiringObjs, engineWait, err := engineInputs(cmd.Context(), newEngine, cfg.Spec.Engine)
	if err != nil {
		return err
	}
	applier, client, err := bootstrapApplier(cmd.Context(), cfg, newProvisioner, kubeconfigPath, contextName)
	if err != nil {
		return err
	}
	out := bootstrapOutcome{cfg: cfg, domain: gatewayDomain(cfg)}
	out.ensured, err = ensureCAMaterial(cmd.Context(), client.Dynamic(), cfg, out.domain, deps)
	if err != nil {
		return err
	}
	install, err := engineInstall(cmd.Context(), out, wiringObjs, engineWait)
	if err != nil {
		return err
	}
	if err := runInstall(cmd.Context(), applier, install, timeout); err != nil {
		return err
	}
	return finishBootstrap(cmd, spliceCubeDomain(client.Dynamic()), out, deps)
}

// engineInstall composes everything one install applies: the engine
// substrate, the ordered prerequisite units the list resolved to, and the
// driver's sync wiring with its reconciliation inputs.
func engineInstall(ctx context.Context, out bootstrapOutcome, wiring []*unstructured.Unstructured, wait bootstrap.EngineWait) (bootstrap.EngineInstall, error) {
	substrateObjs, err := substrate.Objects()
	if err != nil {
		return bootstrap.EngineInstall{}, err
	}
	units, err := prerequisiteUnits(ctx, prereqInputs{
		units:   out.cfg.Spec.Prerequisites,
		domain:  out.domain,
		ensured: out.ensured,
	})
	if err != nil {
		return bootstrap.EngineInstall{}, err
	}
	return bootstrap.EngineInstall{
		Substrate:     substrateObjs,
		Prerequisites: units,
		Wiring:        wiring,
		Wait:          wait,
	}, nil
}

// runInstall runs the install under the CLI's --timeout as one total
// readiness budget, which is the contract bootstrap declares for its phases.
func runInstall(ctx context.Context, applier *bootstrap.Applier, install bootstrap.EngineInstall, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return applier.InstallEngine(ctx, install)
}

// engineInputs composes the engine driver's contribution to bootstrap: the
// injected factory selects the driver from the validated provider (an
// absent spec.engine means the flux default), then the driver supplies the
// sync wiring to apply and the reconciliation inputs for the phased waits.
func engineInputs(ctx context.Context, newEngine engineFactory, engineSpec *v1alpha1.EngineSpec) ([]*unstructured.Unstructured, bootstrap.EngineWait, error) {
	spec := v1alpha1.EngineSpec{Provider: v1alpha1.EngineProviderFlux}
	if engineSpec != nil {
		spec = *engineSpec
	}
	drv, err := newEngine(spec.Provider)
	if err != nil {
		return nil, bootstrap.EngineWait{}, err
	}
	wiring, err := drv.SourceObjects(ctx, spec)
	if err != nil {
		return nil, bootstrap.EngineWait{}, err
	}
	engineObjs, err := drv.EngineObjects(ctx, spec)
	if err != nil {
		return nil, bootstrap.EngineWait{}, err
	}
	return wiring, bootstrap.EngineWait{Reconciled: drv.Reconciled, EngineObjects: engineObjs}, nil
}

// engineVersion returns the requested engine version, empty when the spec or
// its version is absent.
func engineVersion(engine *v1alpha1.EngineSpec) string {
	if engine == nil {
		return ""
	}
	return engine.Version
}

// gatewayDomain resolves the cube's base domain. An absent spec.gateway means
// the fabric installed with defaults, and api's Default() fills only a
// sub-struct the user actually wrote — the engineInputs precedent, applied to
// the same absence. The base domain is api's constant, never respelled here.
func gatewayDomain(cfg *v1alpha1.Config) string {
	if cfg.Spec.Gateway != nil && cfg.Spec.Gateway.Domain != "" {
		return cfg.Spec.Gateway.Domain
	}
	return cfg.Name + "." + v1alpha1.DefaultBaseDomain
}

// bootstrapApplier resolves the cube's kubeconfig target and context via the
// cluster report, reads the bytes at the edge, and injects the constructed
// client-go interfaces (dynamic client + REST mapper) into a bootstrap
// Applier. The client is returned beside it: the edge's own two cluster
// round-trips — the CA Secret read and the CoreDNS rewrite — are not
// bootstrap operations and go through the same client.
func bootstrapApplier(ctx context.Context, cfg *v1alpha1.Config, newProvisioner provisionerFactory, kubeconfigPath, contextName string) (*bootstrap.Applier, *kube.Client, error) {
	p, err := newProvisioner(cfg.Spec.Cluster.Provider)
	if err != nil {
		return nil, nil, err
	}
	rep, err := cluster.Status(ctx, p, cluster.StatusOptions{
		Name:           cfg.Name,
		ContextName:    contextName,
		KubeconfigPath: kubeconfigPath,
	})
	if err != nil {
		return nil, nil, err
	}
	raw, err := os.ReadFile(rep.KubeconfigPath)
	if err != nil {
		return nil, nil, cluster.NewKubeconfigFailedError(fmt.Errorf("read kubeconfig %s: %w", rep.KubeconfigPath, err))
	}
	client, err := kube.New(raw, rep.ContextName)
	if err != nil {
		return nil, nil, err
	}
	// Inventory placement is injected at the edge: the invariant substrate
	// namespace fact, owned by the substrate home.
	return bootstrap.NewApplier(client.Dynamic(), client.RESTMapper(), substrate.Namespace), client, nil
}
