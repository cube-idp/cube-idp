package cli

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	v1alpha1 "github.com/cube-idp/cube-idp/api/config/v1alpha1"
	"github.com/cube-idp/cube-idp/internal/bootstrap"
	"github.com/cube-idp/cube-idp/internal/cluster"
	"github.com/cube-idp/cube-idp/internal/config"
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
// Applier, then hands it the substrate's install objects to apply, record,
// and wait on. bootstrap stays kube-free and engine-free — the edge injects
// content.
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
	applier, err := bootstrapApplier(cmd.Context(), cfg, newProvisioner, kubeconfigPath, contextName)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
	defer cancel()
	// No reconciliation judgment is injected yet: the flux driver that
	// supplies it lands with the M10-C3 wiring move, and until then the
	// post-apply wait phases are skipped — the shipped M7/M9 behavior.
	if err := applier.InstallEngine(ctx, cfg.Spec.Engine, substrateObjs, bootstrap.EngineWait{}); err != nil {
		return err
	}
	renderBootstrapResult(cmd, cfg.Spec.Engine)
	return nil
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
