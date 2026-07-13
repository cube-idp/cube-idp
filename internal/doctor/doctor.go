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
	"os/exec"
	"time"

	"github.com/rafpe/cube-idp/internal/diag"
	"github.com/rafpe/cube-idp/internal/pack"
)

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

// Render prints findings and reports whether any is an error.
func Render(out io.Writer, findings []diag.Finding) bool {
	hasErrors := false
	for _, f := range findings {
		icon := "⚠"
		if f.Severity == diag.SeverityError {
			icon, hasErrors = "✗", true
		}
		fmt.Fprintf(out, "%s %s  %s\n    fix: %s\n", icon, f.Code, f.Message, f.Remediation)
	}
	if len(findings) == 0 {
		fmt.Fprintln(out, "✔ no problems found")
	}
	return hasErrors
}

// ClusterProbeTimeout bounds the cluster-side portion of doctor (provider
// Diagnose + engine Health) — doctor must never hang on a dead apiserver.
const ClusterProbeTimeout = 15 * time.Second
