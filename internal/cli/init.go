package cli

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"

	"github.com/spf13/cobra"

	v1alpha1 "github.com/cube-idp/cube-idp/api/config/v1alpha1"
	"github.com/cube-idp/cube-idp/internal/cluster"
	kindprov "github.com/cube-idp/cube-idp/internal/cluster/kind"
	"github.com/cube-idp/cube-idp/internal/config"
)

// provisionerFactory maps a validated provider to its driver. Driver
// selection stays out of domain packages to keep them import-cycle-free;
// the factory is injected into newInitCmd so tests pass a mock seam
// instead of mutating package state.
type provisionerFactory func(p v1alpha1.ClusterProvider) (cluster.Provisioner, error)

func defaultProvisioner(p v1alpha1.ClusterProvider) (cluster.Provisioner, error) {
	switch p {
	case v1alpha1.ClusterProviderKind:
		return kindprov.New()
	default:
		return nil, cluster.NewUnsupportedProviderError(string(p))
	}
}

func newInitCmd(newProvisioner provisionerFactory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Create the cluster from the Config and install its kubeconfig context",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runInit(cmd, newProvisioner)
		},
	}
	cmd.Flags().String("kubeconfig", "",
		"write the kubeconfig to this file instead of merging into the default location")
	cmd.Flags().String("kubeconfig-context-name", "",
		"override the generated context name (default: cube-idp.dev/<cluster-name>)")
	cmd.Flags().String("name", "",
		"cube name for a scaffolded config (must match metadata.name when the file already exists)")
	return cmd
}

func runInit(cmd *cobra.Command, newProvisioner provisionerFactory) error {
	path, _ := cmd.Flags().GetString("config")
	nameFlag, _ := cmd.Flags().GetString("name")
	kubeconfigPath, _ := cmd.Flags().GetString("kubeconfig")
	contextName, _ := cmd.Flags().GetString("kubeconfig-context-name")

	if err := scaffoldIfAbsent(cmd.OutOrStdout(), path, nameFlag); err != nil {
		return err
	}
	cfg, err := config.LoadFile(path)
	if err != nil {
		return err
	}
	// Mismatch only: a --name equal to the document's metadata.name is a
	// no-op, keeping init --name <x> idempotent. Flags never mutate an
	// existing config.
	if nameFlag != "" && cfg.Name != nameFlag {
		return config.NewNameConflictError(path, cfg.Name, nameFlag)
	}
	if cfg.Spec.Cluster == nil {
		return cluster.NewNoClusterConfiguredError()
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

// scaffoldIfAbsent creates the config document when path does not exist:
// metadata.name from nameFlag if set, else a generated docker-style name.
// An existing file is left untouched, and any other stat failure falls
// through to the loader, which reports it with path context. A concurrent
// creation between the check and the O_EXCL write surfaces as the
// scaffold's own already-exists error rather than clobbering the file.
func scaffoldIfAbsent(stdout io.Writer, path, nameFlag string) error {
	if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	name := nameFlag
	if name == "" {
		name = config.GenerateName()
	}
	if err := config.ScaffoldFile(path, name); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(stdout, "scaffolded %s — cube %q\n", path, name)
	return nil
}
