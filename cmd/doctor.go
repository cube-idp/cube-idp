package cmd

import (
	"context"
	"errors"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/cube-idp/cube-idp/internal/apply"
	"github.com/cube-idp/cube-idp/internal/cluster"
	"github.com/cube-idp/cube-idp/internal/config"
	"github.com/cube-idp/cube-idp/internal/diag"
	"github.com/cube-idp/cube-idp/internal/doctor"
	enginefactory "github.com/cube-idp/cube-idp/internal/engine/factory"
	"github.com/cube-idp/cube-idp/internal/trust"
)

func newDoctorCmd() *cobra.Command {
	var file string
	var output string
	c := &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose the host and (if reachable) the cluster; exit 1 on errors",
		RunE: func(c *cobra.Command, _ []string) error {
			jsonDoc, err := wantJSONDoc(output)
			if err != nil {
				return err
			}
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

			if jsonDoc {
				if writeDoctorJSON(out, findings) {
					os.Exit(1)
				}
				return nil
			}
			if doctor.Render(out, findings) {
				os.Exit(1)
			}
			return nil
		},
	}
	c.Flags().StringVarP(&file, "file", "f", "cube.yaml", "path to cube.yaml")
	addOutputFlag(c, &output)
	return c
}

// doctorDoc is the gh-style doctor document (design doc §10): the findings
// array with codes and severities — CI-annotation gold — plus the overall
// errors verdict that drives the exit code.
type doctorDoc struct {
	jsonDocHead
	Findings []doctorFinding `json:"findings"`
	Errors   bool            `json:"errors"`
}

type doctorFinding struct {
	Code        string `json:"code"`
	Severity    string `json:"severity"`
	Message     string `json:"message"`
	Remediation string `json:"remediation"`
}

// writeDoctorJSON emits the doctor document and reports whether any finding is
// an error (so cmd keeps the os.Exit(1) semantics unchanged across all modes).
func writeDoctorJSON(out io.Writer, findings []diag.Finding) bool {
	doc := doctorDoc{jsonDocHead: jsonDocHead{V: docSchemaVersion}, Findings: make([]doctorFinding, 0, len(findings))}
	for _, f := range findings {
		if f.Severity == diag.SeverityError {
			doc.Errors = true
		}
		doc.Findings = append(doc.Findings, doctorFinding{
			Code: string(f.Code), Severity: string(f.Severity), Message: f.Message, Remediation: f.Remediation,
		})
	}
	// A JSON document must still be emitted even on marshal failure paths; the
	// error is a programming error here (all fields are plain strings).
	_ = writeJSONDoc(out, doc)
	return doc.Errors
}
