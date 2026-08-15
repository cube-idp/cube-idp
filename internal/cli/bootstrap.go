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

// runBootstrap composes the bootstrap at the CLI edge: it builds the kube
// clients (construction confined to internal/kube) and injects them into a
// bootstrap Applier, then applies the embedded Flux, records the inventory,
// and waits for the bootstrap kind-set. bootstrap stays kube-free.
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
	applier, err := bootstrapApplier(cmd.Context(), cfg, newProvisioner, kubeconfigPath, contextName)
	if err != nil {
		return err
	}
	objs, err := bootstrap.FluxObjects()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
	defer cancel()
	if err := applier.Install(ctx, objs); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(),
		"flux %s installed — %d objects applied, inventory recorded\n", bootstrap.FluxVersion, len(objs))
	return nil
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
	return bootstrap.NewApplier(client.Dynamic(), client.RESTMapper()), nil
}
