package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/cube-idp/cube-idp/internal/cluster"
	"github.com/cube-idp/cube-idp/internal/config"
)

func newStatusCmd(newProvisioner provisionerFactory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Report whether the cluster exists and its kubeconfig context is installed",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runStatus(cmd, newProvisioner)
		},
	}
	cmd.Flags().String("kubeconfig", "",
		"inspect this kubeconfig file instead of the default location")
	cmd.Flags().String("kubeconfig-context-name", "",
		"override the context name to look for (default: cube-idp.dev/<cluster-name>)")
	return cmd
}

// runStatus is read-only and exits 0 whenever the report succeeds — an
// absent cluster or uninstalled context is a finding, not a failure.
func runStatus(cmd *cobra.Command, newProvisioner provisionerFactory) error {
	path, _ := cmd.Flags().GetString("config")
	kubeconfigPath, _ := cmd.Flags().GetString("kubeconfig")
	contextName, _ := cmd.Flags().GetString("kubeconfig-context-name")

	cfg, err := config.LoadFile(path)
	if err != nil {
		return err
	}
	if cfg.Spec.Cluster == nil {
		return cluster.NewNoClusterConfiguredError()
	}
	p, err := newProvisioner(cfg.Spec.Cluster.Provider)
	if err != nil {
		return err
	}
	rep, err := cluster.Status(cmd.Context(), p, cluster.StatusOptions{
		Name:           cfg.Name,
		ContextName:    contextName,
		KubeconfigPath: kubeconfigPath,
	})
	if err != nil {
		return err
	}
	clusterState := "not found"
	if rep.ClusterExists {
		clusterState = "exists"
	}
	contextState := "not installed"
	if rep.ContextInstalled {
		contextState = "installed in " + rep.KubeconfigPath
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "cluster %q: %s\n", cfg.Name, clusterState)
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "kubeconfig context %q: %s\n", rep.ContextName, contextState)
	return nil
}
