package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/cube-idp/cube-idp/internal/cluster"
	"github.com/cube-idp/cube-idp/internal/config"
)

func newDeleteCmd(newProvisioner provisionerFactory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete",
		Short: "Delete the cluster from the Config and remove its kubeconfig context",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDelete(cmd, newProvisioner)
		},
	}
	cmd.Flags().String("kubeconfig", "",
		"clean up this kubeconfig file instead of the default location")
	cmd.Flags().String("kubeconfig-context-name", "",
		"override the context name to remove (default: cube-idp.dev/<cluster-name>)")
	return cmd
}

// runDelete resolves the cube from the config document — the single
// source of truth, so there is no --name and never any scaffolding: a
// missing config file is the loader's coded error, not a new cube.
func runDelete(cmd *cobra.Command, newProvisioner provisionerFactory) error {
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
	changed, err := cluster.Delete(cmd.Context(), p, cluster.DeleteOptions{
		Name:           cfg.Name,
		ContextName:    contextName,
		KubeconfigPath: kubeconfigPath,
	})
	if err != nil {
		return err
	}
	name := contextName
	if name == "" {
		name = cluster.ContextName(cfg.Name)
	}
	if changed {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "cluster %q deleted — kubeconfig context %q removed\n", cfg.Name, name)
		return nil
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "cluster %q deleted — no kubeconfig changes needed\n", cfg.Name)
	return nil
}
