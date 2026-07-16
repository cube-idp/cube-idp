// Package doctor implements cube-idp's preflight and health diagnosis
// (spec §4.1): runtime present, ports free, disk space, inotify limits,
// git-CLI availability for git-sourced packs, plus the providers' Diagnose
// and the engine's Health — every finding a typed CUBE code with a
// remediation.
package doctor

import (
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"

	lipgloss "charm.land/lipgloss/v2"

	"github.com/cube-idp/cube-idp/internal/diag"
	"github.com/cube-idp/cube-idp/internal/pack"
	"github.com/cube-idp/cube-idp/internal/ui"
	"github.com/cube-idp/cube-idp/internal/ui/theme"
)

// th is the adaptive palette for doctor's styled render — detected once,
// dark on any doubt; the styled path only engages on a real TTY.
var th = theme.Detect(os.Stdin, os.Stdout)

// portProbeTimeout bounds the localhost dial CheckPortFree uses to detect a
// listener — generous for any real service, short enough not to stall doctor.
const portProbeTimeout = 300 * time.Millisecond

// CheckRuntime looks for a container runtime CLI on PATH — the same set the
// kind provider auto-detects (docker, podman, nerdctl).
func CheckRuntime() *diag.Finding {
	for _, bin := range []string{"docker", "podman", "nerdctl"} {
		if _, err := exec.LookPath(bin); err == nil {
			return nil
		}
	}
	return &diag.Finding{Code: diag.CodeDoctorRuntime, Severity: diag.SeverityError,
		Message:     "no container runtime found on PATH (docker, podman, or nerdctl)",
		Remediation: "install Docker Desktop, Podman, or nerdctl and ensure it is on PATH"}
}

// CheckPortFree probes the gateway host port. Detection dials 127.0.0.1
// rather than attempting to Listen: on BSD-family stacks (darwin included)
// SO_REUSEADDR lets a wildcard Listen succeed even while a loopback-bound
// squatter already holds the port (and vice versa), so a bind-based probe
// silently misses real conflicts on this platform family. Dialing sidesteps
// that — a listener on either 127.0.0.1:port or 0.0.0.0:port answers a
// loopback connection the same way.
//
// When the cluster already exists, the gateway itself legitimately holds
// the port — downgrade to a warning instead of lying about a conflict.
func CheckPortFree(port int, clusterExists bool) *diag.Finding {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), portProbeTimeout)
	if err != nil {
		return nil // nothing answers on localhost: the port is free
	}
	_ = conn.Close()
	sev, msg := diag.SeverityError, fmt.Sprintf("port %d is already in use", port)
	if clusterExists {
		sev, msg = diag.SeverityWarning, fmt.Sprintf("port %d is in use (expected: the cube's gateway binds it)", port)
	}
	return &diag.Finding{Code: diag.CodeDoctorPort, Severity: sev, Message: msg,
		Remediation: fmt.Sprintf("if this is not cube-idp's gateway, stop whatever binds port %d or change spec.gateway.port", port)}
}

// CheckGitCLI warns when any pack ref needs the git CLI to fetch (the bare
// git grammar <host>/<org>/<repo>@<rev>, or an explicit git:: getter form)
// but git isn't on PATH — pack fetch would otherwise fail with a getter
// error that doesn't name the real cause.
func CheckGitCLI(refs []string) *diag.Finding {
	needsGit := false
	for _, r := range refs {
		if pack.NeedsGitCLI(r) {
			needsGit = true
			break
		}
	}
	if !needsGit {
		return nil
	}
	if _, err := exec.LookPath("git"); err == nil {
		return nil
	}
	return &diag.Finding{Code: diag.CodeDoctorGitCLI, Severity: diag.SeverityWarning,
		Message:     "git sources configured but git CLI not found — pack fetch will fail",
		Remediation: "install git and ensure it is on PATH"}
}

// Render prints findings and reports whether any is an error. The ✔/✗/⚠
// glyphs go through ui.Printer.Glyph so doctor, status, and get secrets
// share one visual language (Task 15.3b) — in ModePlain (every existing
// test, since none writes to a real terminal) Glyph returns the same bare
// character this function printed inline before, so the literal output is
// byte-frozen (design doc §8 item 4). ModeStyled (a real terminal only)
// gets the stage-B severity-grouped render (§10).
func Render(out io.Writer, findings []diag.Finding) bool {
	p := ui.NewFor(out)
	hasErrors := false
	for _, f := range findings {
		if f.Severity == diag.SeverityError {
			hasErrors = true
		}
	}
	if p.Styled() {
		renderStyled(p, findings, hasErrors)
		return hasErrors
	}
	for _, f := range findings {
		icon := ui.GlyphWarn
		if f.Severity == diag.SeverityError {
			icon = ui.GlyphErr
		}
		fmt.Fprintf(out, "%s %s  %s\n    fix: %s\n", p.Glyph(icon), f.Code, f.Message, f.Remediation)
	}
	if len(findings) == 0 {
		fmt.Fprintf(out, "%s no problems found\n", p.Glyph(ui.GlyphOK))
	}
	return hasErrors
}

// doctorPanelStyle keeps its own colorless rounded border: doctor's panels
// group ALL severities (warnings, notes), so theme.ErrPanel's red border
// would be wrong — and a colorless border duplicates no palette value.
var doctorPanelStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)

// renderStyled is the stage-B rich static doctor (design doc §10): findings
// grouped by severity into bordered sections, each finding a glyph-led line
// with its CUBE code and message, remediation kept in copy-paste-safe plain
// text (only dimmed, never restyled inside the string), and a final one-line
// verdict. Transient static output — no live view.
func renderStyled(p *ui.Printer, findings []diag.Finding, hasErrors bool) {
	out := p.Out()
	if len(findings) == 0 {
		fmt.Fprintf(out, "%s %s\n", p.Glyph(ui.GlyphOK), th.Section.Render("no problems found"))
		return
	}
	groups := []struct {
		title    string
		severity diag.Severity
		glyph    string
	}{
		{"Errors", diag.SeverityError, ui.GlyphErr},
		{"Warnings", diag.SeverityWarning, ui.GlyphWarn},
		{"Notes", diag.SeverityInfo, ui.GlyphOK},
	}
	for _, g := range groups {
		var body strings.Builder
		n := 0
		for _, f := range findings {
			if f.Severity != g.severity {
				continue
			}
			if n > 0 {
				body.WriteString("\n")
			}
			n++
			fmt.Fprintf(&body, "%s %s  %s\n    %s %s",
				p.Glyph(g.glyph), f.Code, f.Message, th.ErrLabel.Render("fix:"), f.Remediation)
		}
		if n == 0 {
			continue
		}
		fmt.Fprintln(out, th.Section.Render(g.title))
		fmt.Fprintln(out, doctorPanelStyle.Render(body.String()))
	}
	verdict := "no errors — you are good to go"
	glyph := ui.GlyphOK
	if hasErrors {
		verdict, glyph = "errors found — resolve the items above before `cube-idp up`", ui.GlyphErr
	}
	fmt.Fprintf(out, "%s %s\n", p.Glyph(glyph), th.Section.Render(verdict))
}

// ClusterProbeTimeout bounds the cluster-side portion of doctor (provider
// Diagnose + engine Health) — doctor must never hang on a dead apiserver.
const ClusterProbeTimeout = 15 * time.Second
