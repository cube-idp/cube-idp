package cmd

import (
	"context"
	"errors"
	"os"

	"github.com/spf13/cobra"

	"github.com/rafpe/cube-idp/internal/apply"
	"github.com/rafpe/cube-idp/internal/cluster"
	"github.com/rafpe/cube-idp/internal/config"
	"github.com/rafpe/cube-idp/internal/diag"
	"github.com/rafpe/cube-idp/internal/doctor"
	enginefactory "github.com/rafpe/cube-idp/internal/engine/factory"
	"github.com/rafpe/cube-idp/internal/trust"
)

func newDoctorCmd() *cobra.Command {
	var file string
	c := &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose the host and (if reachable) the cluster; exit 1 on errors",
		RunE: func(c *cobra.Command, _ []string) error {
			out := c.OutOrStdout()
			var findings []diag.Finding

			cube, err := config.Load(file)
			if err != nil {
				// A broken config is itself a finding; host checks still run
				// against the D9 default profile rather than bailing out.
				var de *diag.Error
				if errors.As(err, &de) {
					findings = append(findings, diag.Finding{Code: de.Code, Severity: diag.SeverityError,
						Message: de.Summary, Remediation: de.Remediation})
				}
				cube = config.Default("dev")
			}

			// doctor is read-only: unlike Ensure, it must never CREATE a
			// missing cluster (cmd/status.go's requireClusterExists guards the
			// same hazard for `status`). It only probes the cluster side
			// below when one already exists.
			clusterExists := false
			prov, provErr := cluster.New(cube.Spec.Cluster, cube.Spec.Gateway)
			if provErr == nil {
				clusterExists, _ = prov.Exists(c.Context(), cube.Metadata.Name)
			}

			if cube.Spec.Cluster.Provider == "kind" {
				if f := doctor.CheckRuntime(); f != nil {
					findings = append(findings, *f)
				}
			}
			if f := doctor.CheckPortFree(cube.Spec.Gateway.Port, clusterExists); f != nil {
				findings = append(findings, *f)
			}
			if dir, err := trust.Dir(); err == nil {
				if f := doctor.CheckDiskSpace(dir, 5<<30); f != nil {
					findings = append(findings, *f)
				}
			}
			findings = append(findings, doctor.CheckInotify()...)

			// scan every ref `up` would fetch: spec.packs plus the gateway
			// pack (its ref override may also be a git source)
			refs := make([]string, 0, len(cube.Spec.Packs)+1)
			for _, p := range cube.Spec.Packs {
				refs = append(refs, p.Ref)
			}
			refs = append(refs, cube.Spec.Gateway.PackRef())
			if f := doctor.CheckGitCLI(refs); f != nil {
				findings = append(findings, *f)
			}

			if provErr == nil {
				findings = append(findings, prov.Diagnose(c.Context(), cube.Metadata.Name)...)
			}

			if provErr == nil && clusterExists {
				ctx, cancel := context.WithTimeout(c.Context(), doctor.ClusterProbeTimeout)
				defer cancel()
				if conn, err := prov.Ensure(ctx, cube.Metadata.Name, cube.Spec.Cluster); err == nil {
					if a, err := apply.New(conn.REST, cube.Metadata.Name); err == nil {
						if eng, err := enginefactory.New(cube.Spec.Engine.Type); err == nil {
							if comps, err := eng.Health(ctx, a); err == nil {
								for _, comp := range comps {
									if !comp.Ready {
										findings = append(findings, diag.Finding{Code: diag.CodeEngineHealthTimeout,
											Severity: diag.SeverityError, Message: comp.Name + " not ready: " + comp.Message,
											Remediation: "re-run `cube-idp up`; inspect the component with kubectl"})
									}
								}
							}
						}
					}
				} else {
					var de *diag.Error
					if errors.As(err, &de) {
						findings = append(findings, diag.Finding{Code: de.Code, Severity: diag.SeverityError,
							Message: de.Summary, Remediation: de.Remediation})
					}
				}
			}

			if doctor.Render(out, findings) {
				os.Exit(1)
			}
			return nil
		},
	}
	c.Flags().StringVarP(&file, "file", "f", "cube.yaml", "path to cube.yaml")
	return c
}
