package cmd

import (
	"context"
	"time"

	"github.com/spf13/cobra"

	"github.com/rafpe/cube-idp/internal/apply"
	"github.com/rafpe/cube-idp/internal/cluster"
	"github.com/rafpe/cube-idp/internal/config"
	enginefactory "github.com/rafpe/cube-idp/internal/engine/factory"
	"github.com/rafpe/cube-idp/internal/trust"
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
				// Ensure would CREATE a missing kind cluster — never as a
				// side effect of down --keep-cluster.
				if err := requireClusterExists(c.Context(), prov, cube.Spec.Cluster.Provider, cube.Metadata.Name); err != nil {
					return err
				}
				// "no infinite spinner": an unreachable existing cluster must
				// not stall down indefinitely (mirrors status's connect timeout).
				ensureCtx, cancel := context.WithTimeout(c.Context(), 3*time.Minute)
				conn, err := prov.Ensure(ensureCtx, cube.Metadata.Name, cube.Spec.Cluster)
				cancel()
				if err != nil {
					return err
				}
				a, err := apply.New(conn.REST, cube.Metadata.Name)
				if err != nil {
					return err
				}
				// Two-phase teardown: first the engine deletes its delivered
				// sources and waits for their prune finalizers (so flux
				// removes the workloads it delivered while its controllers
				// are still alive), then the inventory cascade removes
				// everything else — DeleteAll skips the already-gone engine
				// objects via its IsNotFound/NoMatch handling.
				eng, err := enginefactory.New(cube.Spec.Engine.Type)
				if err != nil {
					return err
				}
				if err := eng.Uninstall(c.Context(), a, 5*time.Minute); err != nil {
					return err
				}
				// D6: revert the CoreDNS rewrite before tearing the rest down
				// — the cluster (and CoreDNS with it) survives this path, so
				// nothing else undoes it.
				if err := trust.RemoveCoreDNSRewrite(c.Context(), a.Client(), 2*time.Minute); err != nil {
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
