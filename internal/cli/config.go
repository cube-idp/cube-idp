package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"

	v1alpha1 "github.com/cube-idp/cube-idp/api/config/v1alpha1"
	"github.com/cube-idp/cube-idp/internal/cluster"
	"github.com/cube-idp/cube-idp/internal/config"
)

func newConfigCmd(newProvisioner provisionerFactory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Inspect and validate the Config document",
	}
	cmd.AddCommand(newConfigValidateCmd(newProvisioner), newConfigShowCmd())
	return cmd
}

func newConfigValidateCmd(newProvisioner provisionerFactory) *cobra.Command {
	return &cobra.Command{
		Use:   "validate",
		Short: "Load, default, and validate the Config; report every problem",
		RunE: func(cmd *cobra.Command, _ []string) error {
			path, _ := cmd.Flags().GetString("config")
			cfg, err := config.LoadFile(path)
			if err != nil {
				return err
			}
			if err := validateProviderSpec(cfg, newProvisioner); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "config %q is valid\n", cfg.Name)
			return nil
		},
	}
}

// validateProviderSpec surfaces provider-side forProvider validation via
// the seam's optional SpecValidator capability. The composition lives
// here at the CLI edge so internal/config never imports internal/cluster.
// A driver without the capability validates nothing — that is not an
// error. Failures keep the provider domain's code (CUBE-CLU-003, exit 1),
// distinct from document errors (CUBE-CFG-*, exit 2).
func validateProviderSpec(cfg *v1alpha1.Config, newProvisioner provisionerFactory) error {
	if cfg.Spec.Cluster == nil {
		return nil
	}
	p, err := newProvisioner(cfg.Spec.Cluster.Provider)
	if err != nil {
		return err
	}
	v, ok := p.(cluster.SpecValidator)
	if !ok {
		return nil
	}
	return v.ValidateSpec(cluster.Spec{Name: cfg.Name, ForProvider: cfg.Spec.Cluster.ForProvider})
}

func newConfigShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Print the loaded, defaulted Config back as YAML",
		RunE: func(cmd *cobra.Command, _ []string) error {
			path, _ := cmd.Flags().GetString("config")
			cfg, err := config.LoadFile(path)
			if err != nil {
				return err
			}
			out, err := yaml.Marshal(cfg)
			if err != nil {
				return fmt.Errorf("render config: %w", err)
			}
			_, _ = fmt.Fprint(cmd.OutOrStdout(), string(out))
			return nil
		},
	}
}
