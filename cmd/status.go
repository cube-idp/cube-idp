package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/rafpe/cube-idp/internal/apply"
	"github.com/rafpe/cube-idp/internal/cluster"
	"github.com/rafpe/cube-idp/internal/config"
	"github.com/rafpe/cube-idp/internal/diag"
	enginefactory "github.com/rafpe/cube-idp/internal/engine/factory"
)

const statusClusterTimeout = 3 * time.Minute

func newStatusCmd() *cobra.Command {
	var file string
	c := &cobra.Command{
		Use:   "status",
		Short: "Report cluster connectivity, engine-reported component health, and inventory size",
		RunE: func(c *cobra.Command, _ []string) error {
			cube, err := config.Load(file)
			if err != nil {
				return err
			}
			prov, err := cluster.New(cube.Spec.Cluster, cube.Spec.Gateway)
			if err != nil {
				return err
			}
			// status is read-only: Ensure would CREATE a missing kind cluster.
			if err := requireClusterExists(c.Context(), prov, cube.Spec.Cluster.Provider, cube.Metadata.Name); err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(c.Context(), statusClusterTimeout)
			conn, err := prov.Ensure(ctx, cube.Metadata.Name, cube.Spec.Cluster)
			cancel()
			if err != nil {
				return err
			}
			a, err := apply.New(conn.REST, cube.Metadata.Name)
			if err != nil {
				return err
			}
			eng, err := enginefactory.New(cube.Spec.Engine.Type)
			if err != nil {
				return err
			}

			out := c.OutOrStdout()
			health, err := eng.Health(c.Context(), a)
			if err != nil {
				return err
			}
			allReady := true
			for _, h := range health {
				if h.Ready {
					fmt.Fprintf(out, "✔ %s Ready\n", h.Name)
					continue
				}
				allReady = false
				fmt.Fprintf(out, "✗ %s %s\n", h.Name, h.Message)
			}

			inventory, err := a.LoadInventory(c.Context())
			if err != nil {
				return err
			}
			fmt.Fprintf(out, "\n%d object(s) in inventory\n", len(inventory))

			if len(health) == 0 || !allReady {
				return diag.New("CUBE-3004", "one or more components are not ready",
					"inspect the components listed above with kubectl; re-run `cube-idp up` if needed")
			}
			return nil
		},
	}
	c.Flags().StringVarP(&file, "file", "f", "cube.yaml", "path to cube.yaml")
	return c
}
