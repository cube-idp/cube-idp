package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	v1alpha1 "github.com/cube-idp/cube-idp/api/config/v1alpha1"
	"github.com/cube-idp/cube-idp/internal/cluster"
	kindprov "github.com/cube-idp/cube-idp/internal/cluster/kind"
	"github.com/cube-idp/cube-idp/internal/config"
)

// newProvisioner maps a validated provider to its driver. Driver
// selection stays out of domain packages to keep them import-cycle-free;
// a var so tests inject a mock seam.
var newProvisioner = func(p v1alpha1.ClusterProvider) (cluster.Provisioner, error) {
	switch p {
	case v1alpha1.ClusterProviderKind:
		return kindprov.New()
	default:
		return nil, cluster.ErrUnsupportedProvider(string(p))
	}
}

func newInitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Create the cluster from the Config and install its kubeconfig context",
		RunE:  runInit,
	}
	cmd.Flags().String("kubeconfig", "",
		"write the kubeconfig to this file instead of merging into the default location")
	cmd.Flags().String("kubeconfig-context-name", "",
		"override the generated context name (default: cube-idp.dev/<cluster-name>)")
	return cmd
}

func runInit(cmd *cobra.Command, _ []string) error {
	path, _ := cmd.Flags().GetString("config")
	kubeconfigPath, _ := cmd.Flags().GetString("kubeconfig")
	contextName, _ := cmd.Flags().GetString("kubeconfig-context-name")

	cfg, err := config.LoadFile(path)
	if err != nil {
		return err
	}
	if cfg.Spec.Cluster == nil {
		return cluster.ErrNoClusterConfigured()
	}
	p, err := newProvisioner(cfg.Spec.Cluster.Provider)
	if err != nil {
		return err
	}
	if err := cluster.Init(cmd.Context(), p, cluster.InitOptions{
		Spec:           cluster.Spec{Name: cfg.Name, ForProvider: cfg.Spec.Cluster.ForProvider},
		ContextName:    contextName,
		KubeconfigPath: kubeconfigPath,
	}); err != nil {
		return err
	}
	name := contextName
	if name == "" {
		name = cluster.ContextName(cfg.Name)
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "cluster %q ready — kubeconfig context %q installed\n", cfg.Name, name)
	return nil
}
