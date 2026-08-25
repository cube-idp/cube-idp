package cli

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	v1alpha1 "github.com/cube-idp/cube-idp/api/config/v1alpha1"
	"github.com/cube-idp/cube-idp/internal/bootstrap"
	"github.com/cube-idp/cube-idp/internal/cluster"
	"github.com/cube-idp/cube-idp/internal/config"
	"github.com/cube-idp/cube-idp/internal/engine/flux"
	"github.com/cube-idp/cube-idp/internal/engine/substrate"
	"github.com/cube-idp/cube-idp/internal/kube"
)

// defaultBootstrapTimeout bounds how long bootstrap waits for the kind-set to
// become ready (images pull, controllers start) before giving up.
const defaultBootstrapTimeout = 5 * time.Minute

func newBootstrapCmd(newProvisioner provisionerFactory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bootstrap",
		Short: "Install the gitops engine (Flux) into the cluster and wait for it to be ready",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runBootstrap(cmd, newProvisioner)
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

// runBootstrap composes the bootstrap at the CLI edge: it asserts the
// requested engine version against the substrate pin, builds the kube clients
// (construction confined to internal/kube) and injects them into a bootstrap
// Applier, then hands it the substrate's install objects, the engine driver's
// sync wiring, and the driver's reconciliation judgment. bootstrap stays
// kube-free and engine-free — the edge injects content and judgment alike.
func runBootstrap(cmd *cobra.Command, newProvisioner provisionerFactory) error {
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
	substrateObjs, err := substrate.Objects()
	if err != nil {
		return err
	}
	wiringObjs, engineWait, err := engineInputs(cmd.Context(), cfg.Spec.Engine)
	if err != nil {
		return err
	}
	applier, err := bootstrapApplier(cmd.Context(), cfg, newProvisioner, kubeconfigPath, contextName)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
	defer cancel()
	if err := applier.InstallEngine(ctx, substrateObjs, wiringObjs, engineWait); err != nil {
		return err
	}
	renderBootstrapResult(cmd, cfg.Spec.Engine)
	return nil
}

// engineInputs composes the engine driver's contribution to bootstrap: the
// sync wiring to apply and the reconciliation inputs for the phased waits.
// The flux driver is constructed directly — the only driver today; the
// provider selection factory (and CUBE-ENG-001) arrives with the CLI wiring
// task of this milestone.
func engineInputs(ctx context.Context, engine *v1alpha1.EngineSpec) ([]*unstructured.Unstructured, bootstrap.EngineWait, error) {
	drv := flux.New()
	spec := v1alpha1.EngineSpec{}
	if engine != nil {
		spec = *engine
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

// renderBootstrapResult reports what bootstrap installed: Flux, and the sync
// source when one is configured.
func renderBootstrapResult(cmd *cobra.Command, engine *v1alpha1.EngineSpec) {
	out := cmd.OutOrStdout()
	if engine != nil && engine.Source != nil && engine.Source.URL != "" {
		_, _ = fmt.Fprintf(out, "flux %s installed — syncing from %s (%s)\n",
			substrate.Version, engine.Source.URL, engine.Source.Kind)
		return
	}
	_, _ = fmt.Fprintf(out, "flux %s installed\n", substrate.Version)
}

// bootstrapApplier resolves the cube's kubeconfig target and context via the
// cluster report, reads the bytes at the edge, and injects the constructed
// client-go interfaces (dynamic client + REST mapper) into a bootstrap Applier.
func bootstrapApplier(ctx context.Context, cfg *v1alpha1.Config, newProvisioner provisionerFactory, kubeconfigPath, contextName string) (*bootstrap.Applier, error) {
	p, err := newProvisioner(cfg.Spec.Cluster.Provider)
	if err != nil {
		return nil, err
	}
	rep, err := cluster.Status(ctx, p, cluster.StatusOptions{
		Name:           cfg.Name,
		ContextName:    contextName,
		KubeconfigPath: kubeconfigPath,
	})
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(rep.KubeconfigPath)
	if err != nil {
		return nil, cluster.NewKubeconfigFailedError(fmt.Errorf("read kubeconfig %s: %w", rep.KubeconfigPath, err))
	}
	client, err := kube.New(raw, rep.ContextName)
	if err != nil {
		return nil, err
	}
	// Inventory placement is injected at the edge: the invariant substrate
	// namespace fact, owned by the substrate home.
	return bootstrap.NewApplier(client.Dynamic(), client.RESTMapper(), substrate.Namespace), nil
}
