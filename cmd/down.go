package cmd

import (
	"time"

	"github.com/spf13/cobra"

	"github.com/rafpe/cube-idp/internal/apply"
	"github.com/rafpe/cube-idp/internal/cluster"
	"github.com/rafpe/cube-idp/internal/config"
)

func newDownCmd() *cobra.Command {
	var file string
	var keepCluster bool
	c := &cobra.Command{
		Use:   "down",
		Short: "Delete everything cube-idp created (inventory-driven cascade), then the cluster",
		RunE: func(c *cobra.Command, _ []string) error {
			cube, err := config.Load(file)
			if err != nil {
				return err
			}
			prov, err := cluster.New(cube.Spec.Cluster, cube.Spec.Gateway)
			if err != nil {
				return err
			}
			// existing clusters: remove only cube-idp-managed resources (spec §4.3)
			if cube.Spec.Cluster.Provider == "existing" || keepCluster {
				conn, err := prov.Ensure(c.Context(), cube.Metadata.Name, cube.Spec.Cluster)
				if err != nil {
					return err
				}
				a, err := apply.New(conn.REST, cube.Metadata.Name)
				if err != nil {
					return err
				}
				return a.DeleteAll(c.Context(), 5*time.Minute)
			}
			// kind: deleting the cluster IS the cascade
			return prov.Delete(c.Context(), cube.Metadata.Name)
		},
	}
	c.Flags().StringVarP(&file, "file", "f", "cube.yaml", "path to cube.yaml")
	c.Flags().BoolVar(&keepCluster, "keep-cluster", false, "delete cube-idp resources but keep the cluster")
	return c
}
