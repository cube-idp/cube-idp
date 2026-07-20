package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cube-idp/cube-idp/internal/apply"
	"github.com/cube-idp/cube-idp/internal/cfgload"
	"github.com/cube-idp/cube-idp/internal/cluster"
	"github.com/cube-idp/cube-idp/internal/config"
	"github.com/cube-idp/cube-idp/internal/diag"
	"github.com/cube-idp/cube-idp/internal/doctor"
	enginefactory "github.com/cube-idp/cube-idp/internal/engine/factory"
	"github.com/cube-idp/cube-idp/internal/ui"
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

			cube, err := cfgload.Load(c.Context(), file)
			if err != nil {
				// A broken config is itself a finding; host checks still run
				// against the default profile rather than bailing out.
				var de *diag.Error
				if errors.As(err, &de) {
					findings = append(findings, diag.Finding{Code: de.Code, Severity: diag.SeverityError,
						Message: de.Summary, Remediation: de.Remediation})
				}
				cube = config.Default("dev")
			}

			// Cluster-side observation first (existence feeds the host
			// checks' port heuristics), through its seam.
			clusterExists, clusterFindings, spokeResults := doctorProbeCluster(c.Context(), cube)

			// The tri-state checklist: every registered check renders exactly
			// one row — passes are shown, not silent. The findings array keeps
			// its original order: config, host checks, cluster-side, spokes.
			// shown. The findings array keeps its pre-U5 order: config,
			// host checks, cluster-side, spokes.
			results := doctor.RunChecks(doctorChecks(cube, clusterExists))
			for _, r := range results {
				findings = append(findings, r.Findings...)
			}
			findings = append(findings, clusterFindings...)
			results = append(results, spokeResults...)
			for _, r := range spokeResults {
				findings = append(findings, r.Findings...)
			}

			if jsonDoc {
				if writeDoctorJSON(out, findings, results) {
					return errExitCode(1)
				}
				return nil
			}
			renderDoctorChecklist(out, results)
			if doctor.Render(out, findings) {
				return errExitCode(1)
			}
			return nil
		},
	}
	c.Flags().StringVarP(&file, "file", "f", "cube.yaml", "path to cube.yaml")
	addOutputFlag(c, &output)
	return c
}

// doctorChecks assembles the host-side checklist — a package-level seam
// (the statusConnect pattern) so command tests stub the check set.
var doctorChecks = doctor.All

// doctorProbeCluster performs every cluster-side observation doctor makes —
// seamed like doctorChecks so command tests never touch docker,
// kubeconfigs, or the network.
var doctorProbeCluster = probeDoctorCluster

// probeDoctorCluster resolves the provider, reports whether the cluster
// already exists (doctor is read-only: unlike Ensure it must never CREATE a
// missing cluster — cmd/status.go's requireClusterExists guards the same
// hazard for `status`), collects the provider's Diagnose findings and, when
// the cluster is reachable, the engine-health findings plus the S4
// spoke-reachability check (one checklist row over CUBE-8006 findings). The spoke
// check is assembled here and not in doctor.All because it needs the live
// cluster client; it is registered only when spokes are declared AND the
// hub answered — a checklist row always means "this was probed now".
func probeDoctorCluster(ctx context.Context, cube *config.Cube) (bool, []diag.Finding, []doctor.CheckResult) {
	var findings []diag.Finding
	prov, provErr := cluster.New(cube.Spec.Cluster, cube.Spec.Gateway)
	if provErr != nil {
		return false, nil, nil
	}
	clusterExists, _ := prov.Exists(ctx, cube.Metadata.Name)
	findings = append(findings, prov.Diagnose(ctx, cube.Metadata.Name)...)
	if !clusterExists {
		return clusterExists, findings, nil
	}
	pctx, cancel := context.WithTimeout(ctx, doctor.ClusterProbeTimeout)
	defer cancel()
	conn, err := prov.Ensure(pctx, cube.Metadata.Name, cube.Spec.Cluster)
	if err != nil {
		var de *diag.Error
		if errors.As(err, &de) {
			findings = append(findings, diag.Finding{Code: de.Code, Severity: diag.SeverityError,
				Message: de.Summary, Remediation: de.Remediation})
		}
		return clusterExists, findings, nil
	}
	a, err := apply.New(conn.REST, cube.Metadata.Name)
	if err != nil {
		return clusterExists, findings, nil
	}
	if eng, err := enginefactory.New(cube.Spec.Engine); err == nil {
		if comps, err := eng.Health(pctx, a); err == nil {
			for _, comp := range comps {
				if !comp.Ready {
					findings = append(findings, diag.Finding{Code: diag.CodeEngineHealthTimeout,
						Severity: diag.SeverityError, Message: comp.Name + " not ready: " + comp.Message,
						Remediation: "re-run `cube-idp up`; inspect the component with kubectl"})
				}
			}
		}
	}
	var spokeResults []doctor.CheckResult
	if spokes := cube.Spec.Spokes; len(spokes) > 0 {
		// The spoke probe as a checklist row (check id spoke-reachability). Findings
		// stay warnings by design: kind spokes register a
		// docker-network-internal URL this machine may not reach while the
		// hub engine still reconciles them.
		spokeResults = doctor.RunChecks([]doctor.Check{{Name: "spoke-reachability", Run: func() (string, []diag.Finding) {
			if fs := doctor.CheckSpokeReachability(pctx, a.Client(), cube.Spec.Engine.Type, spokes); len(fs) > 0 {
				return "", fs
			}
			return fmt.Sprintf("%d spoke(s) registered and reachable", len(spokes)), nil
		}}})
	}
	return clusterExists, findings, spokeResults
}

// renderDoctorChecklist prints the tri-state checklist: one row per
// executed check. Styled rows pair the theme-colored glyph with the word
// (semantic-color doctrine — the word always accompanies the glyph); plain
// rows carry the ok/warn/fail word alone, no glyphs. The findings render
// (with remediations) and the verdict line follow, unchanged in meaning.
func renderDoctorChecklist(out io.Writer, results []doctor.CheckResult) {
	if len(results) == 0 {
		return
	}
	p := ui.NewFor(out)
	width := 0
	for _, r := range results {
		if len(r.Name) > width {
			width = len(r.Name)
		}
	}
	width += 2
	for _, r := range results {
		word := r.Status()
		line := fmt.Sprintf("%-6s%-*s%s", word, width, r.Name, doctorRowText(r))
		if p.Styled() {
			line = doctorRowGlyph(p, word) + " " + line
		}
		fmt.Fprintln(out, strings.TrimRight(line, " "))
	}
	fmt.Fprintln(out)
}

// doctorRowGlyph maps a row word onto its themed glyph (ok ✔ / warn ⚠ /
// fail ✗) for the styled render.
func doctorRowGlyph(p *ui.Printer, word string) string {
	switch word {
	case "ok":
		return p.Glyph(ui.GlyphOK)
	case "fail":
		return p.Glyph(ui.GlyphErr)
	default:
		return p.Glyph(ui.GlyphWarn)
	}
}

// doctorRowText is one row's right-hand cell: the green detail, or the
// worst finding's message paired with its CUBE code — plus a (+N more)
// marker when a multi-finding check (inotify, spoke-reachability) carries
// siblings. The full finding set always follows in the findings render.
func doctorRowText(r doctor.CheckResult) string {
	w := r.Worst()
	if w == nil {
		return r.Detail
	}
	text := fmt.Sprintf("%s — %s", w.Message, w.Code)
	if n := len(r.Findings) - 1; n > 0 {
		text = fmt.Sprintf("%s (+%d more)", text, n)
	}
	return text
}

// doctorDoc is the gh-style doctor document — a single final object rather
// than a stream, because doctor answers once: the findings
// array with codes and severities — CI-annotation gold — the additive U5
// checks array (one tri-state row per executed check), plus the
// overall errors verdict that drives the exit code.
type doctorDoc struct {
	jsonDocHead
	Findings []doctorFinding `json:"findings"`
	Checks   []doctorCheck   `json:"checks"`
	Errors   bool            `json:"errors"`
}

type doctorFinding struct {
	Code        string `json:"code"`
	Severity    string `json:"severity"`
	Message     string `json:"message"`
	Remediation string `json:"remediation"`
}

// doctorCheck is one checklist row: name is the stable check id, status is
// ok|warn|fail; detail is present on ok rows, code+message (the worst
// finding) on warn/fail rows. The field is purely additive — existing consumers keep
// reading findings/errors unchanged.
type doctorCheck struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Detail  string `json:"detail,omitempty"`
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

// writeDoctorJSON emits the doctor document and reports whether any finding is
// an error (so cmd keeps the exit-1 semantics unchanged across all modes).
func writeDoctorJSON(out io.Writer, findings []diag.Finding, results []doctor.CheckResult) bool {
	doc := doctorDoc{jsonDocHead: jsonDocHead{V: docSchemaVersion},
		Findings: make([]doctorFinding, 0, len(findings)),
		Checks:   make([]doctorCheck, 0, len(results))}
	for _, f := range findings {
		if f.Severity == diag.SeverityError {
			doc.Errors = true
		}
		doc.Findings = append(doc.Findings, doctorFinding{
			Code: string(f.Code), Severity: string(f.Severity), Message: f.Message, Remediation: f.Remediation,
		})
	}
	for _, r := range results {
		row := doctorCheck{Name: r.Name, Status: r.Status(), Detail: r.Detail}
		if w := r.Worst(); w != nil {
			row.Code, row.Message = string(w.Code), w.Message
		}
		doc.Checks = append(doc.Checks, row)
	}
	// A JSON document must still be emitted even on marshal failure paths; the
	// error is a programming error here (all fields are plain strings).
	_ = writeJSONDoc(out, doc)
	return doc.Errors
}
