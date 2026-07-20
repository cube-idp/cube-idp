// Package doctor implements cube-idp's preflight and health diagnosis
// runtime present, ports free, disk space, inotify limits,
// git-CLI availability for git-sourced packs, plus the providers' Diagnose
// and the engine's Health — every finding a typed CUBE code with a
// remediation.
package doctor

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	goruntime "runtime"
	"strings"
	"sync"
	"time"

	lipgloss "charm.land/lipgloss/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cube-idp/cube-idp/internal/config"
	"github.com/cube-idp/cube-idp/internal/diag"
	"github.com/cube-idp/cube-idp/internal/pack"
	"github.com/cube-idp/cube-idp/internal/trust"
	"github.com/cube-idp/cube-idp/internal/ui"
	"github.com/cube-idp/cube-idp/internal/ui/theme"
)

// th is the adaptive palette for doctor's styled render — detected once,
// dark on any doubt; the styled path only engages on a real TTY.
var th = theme.Detect(os.Stdin, os.Stdout)

// portProbeTimeout bounds the localhost dial CheckPortFree uses to detect a
// listener — generous for any real service, short enough not to stall doctor.
const portProbeTimeout = 300 * time.Millisecond

// runtimeBins is the container-runtime CLI set CheckRuntime probes — the
// same set the kind provider auto-detects. Shared with the U5 checklist
// wrapper so the green row can name which binary satisfied the check.
var runtimeBins = []string{"docker", "podman", "nerdctl"}

// CheckRuntime looks for a container runtime CLI on PATH — the same set the
// kind provider auto-detects (docker, podman, nerdctl).
func CheckRuntime() *diag.Finding {
	for _, bin := range runtimeBins {
		if _, err := exec.LookPath(bin); err == nil {
			return nil
		}
	}
	return &diag.Finding{Code: diag.CodeDoctorRuntime, Severity: diag.SeverityError,
		Message:     "no container runtime found on PATH (docker, podman, or nerdctl)",
		Remediation: "install Docker Desktop, Podman, or nerdctl and ensure it is on PATH"}
}

// CheckPortFree probes the gateway's HTTPS host port (spec.gateway.port).
// It is CheckHostPortFree with that field name baked in — kept for the
// existing call sites (init's wizard, doctor's default probe).
func CheckPortFree(port int, clusterExists bool) *diag.Finding {
	return CheckHostPortFree(port, clusterExists, "spec.gateway.port")
}

// CheckHostPortFree probes one required host port; field names the
// cube.yaml field the remediation tells the user to change
// (spec.gateway.port, or spec.gateway.httpPort for U2's opt-in plain-HTTP
// probe). Detection dials 127.0.0.1 rather than attempting to Listen: on
// BSD-family stacks (darwin included) SO_REUSEADDR lets a wildcard Listen
// succeed even while a loopback-bound squatter already holds the port (and
// vice versa), so a bind-based probe silently misses real conflicts on this
// platform family. Dialing sidesteps that — a listener on either
// 127.0.0.1:port or 0.0.0.0:port answers a loopback connection the same
// way.
//
// When the cluster already exists, the gateway itself legitimately holds
// the port — downgrade to a warning instead of lying about a conflict.
func CheckHostPortFree(port int, clusterExists bool, field string) *diag.Finding {
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
		Remediation: fmt.Sprintf("if this is not cube-idp's gateway, stop whatever binds port %d or change %s", port, field)}
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
// share one visual language — in ModePlain (every existing
// test, since none writes to a real terminal) Glyph returns the same bare
// character this function printed inline before, so the literal output is
// byte-frozen: doctor.Render's plain bytes are a pinned contract
// (docs/adr/0024-plain-output-byte-freeze.md). ModeStyled (a real terminal only)
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

// renderStyled is the rich static doctor for styled terminals: findings
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

// SpokeState is one declared spoke's observed hub-side state (S4):
// Registered — the S3 hub registration secret exists in the engine's
// namespace; Reachable — the spoke API server answered GET /readyz using
// that secret's own payload, i.e. exactly the URL and credentials the hub
// engine would use. For kind spokes the payload's server URL is
// docker-network-internal (kind spokes register https://<cluster>-control-plane:6443
// from kind's internal kubeconfig), so a probe from outside that network can
// truthfully report unreachable while the hub engine still reconciles.
type SpokeState struct {
	Name       string
	Provider   string
	Registered bool
	Reachable  bool
}

// spokeProbeTimeout bounds each spoke's /readyz GET (S4: 2 seconds per
// spoke; spokes probe in parallel so a fleet never stalls status).
const spokeProbeTimeout = 2 * time.Second

// spokeHubSecretRef returns the hub registration secret coordinates for one
// spoke — the S3 internal/spoke.HubSecrets contract: cube-idp-spoke-<name>
// in ns "argocd" (engine argocd) or "flux-system" (engine flux).
func spokeHubSecretRef(engineType, spokeName string) (namespace, name string) {
	ns := "flux-system"
	if engineType == "argocd" {
		ns = "argocd"
	}
	return ns, "cube-idp-spoke-" + spokeName
}

// spokeArgocdConfig mirrors the argocd cluster-secret `config` JSON payload
// S3 writes (internal/spoke's unexported argocdClusterConfig — fields
// copied per its handoff).
type spokeArgocdConfig struct {
	BearerToken     string `json:"bearerToken"`
	TLSClientConfig struct {
		CAData []byte `json:"caData"`
	} `json:"tlsClientConfig"`
}

// spokeRESTConfig rebuilds the REST config the hub engine would use from the
// registration secret's own payload: argocd → server + config JSON; flux →
// the `value` kubeconfig.
func spokeRESTConfig(engineType string, sec *corev1.Secret) (*rest.Config, error) {
	if engineType == "argocd" {
		var cc spokeArgocdConfig
		if err := json.Unmarshal(sec.Data["config"], &cc); err != nil {
			return nil, err
		}
		server := string(sec.Data["server"])
		if server == "" {
			return nil, fmt.Errorf("argocd cluster secret %s/%s has no server", sec.Namespace, sec.Name)
		}
		return &rest.Config{
			Host:            server,
			BearerToken:     cc.BearerToken,
			TLSClientConfig: rest.TLSClientConfig{CAData: cc.TLSClientConfig.CAData},
		}, nil
	}
	return clientcmd.RESTConfigFromKubeConfig(sec.Data["value"])
}

// ProbeSpokes observes every declared spoke: Registered from the hub
// registration secret, Reachable from a GET /readyz built from that
// secret's payload — spokeProbeTimeout per spoke, all spokes in parallel.
// A probe failure is a state, never an error: status must render a dead
// spoke, not fail on it.
func ProbeSpokes(ctx context.Context, c client.Client, engineType string, spokes []config.SpokeSpec) []SpokeState {
	states := make([]SpokeState, len(spokes))
	var wg sync.WaitGroup
	for i, sp := range spokes {
		states[i] = SpokeState{Name: sp.Name, Provider: sp.Cluster.Provider}
		ns, name := spokeHubSecretRef(engineType, sp.Name)
		var sec corev1.Secret
		if err := c.Get(ctx, client.ObjectKey{Namespace: ns, Name: name}, &sec); err != nil {
			continue // not registered — nothing to probe
		}
		states[i].Registered = true
		cfg, err := spokeRESTConfig(engineType, &sec)
		if err != nil {
			continue // malformed payload probes as unreachable
		}
		wg.Add(1)
		go func(st *SpokeState) {
			defer wg.Done()
			st.Reachable = spokeReadyz(ctx, cfg)
		}(&states[i])
	}
	wg.Wait()
	return states
}

// spokeReadyz reports whether the API server behind cfg answers GET /readyz
// within spokeProbeTimeout.
func spokeReadyz(ctx context.Context, cfg *rest.Config) bool {
	hc, err := rest.HTTPClientFor(cfg)
	if err != nil {
		return false
	}
	pctx, cancel := context.WithTimeout(ctx, spokeProbeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(pctx, http.MethodGet, strings.TrimSuffix(cfg.Host, "/")+"/readyz", nil)
	if err != nil {
		return false
	}
	resp, err := hc.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// CheckSpokeReachability is doctor's spoke probe (check id:
// spoke-reachability — U5's tri-state checklist will wrap it like the other
// Check* functions): silent when no spokes are declared; every declared
// spoke whose hub registration secret is missing, or whose API server does
// not answer /readyz from this machine, yields one CUBE-8006 warning naming
// it. Warnings, never errors: for kind spokes the registered URL is
// docker-network-internal (kind's internal kubeconfig URL, reachable only on
// the shared docker network), so the hub engine can reconcile a spoke
// this machine cannot connect to.
func CheckSpokeReachability(ctx context.Context, c client.Client, engineType string, spokes []config.SpokeSpec) []diag.Finding {
	if len(spokes) == 0 {
		return nil
	}
	var findings []diag.Finding
	for _, st := range ProbeSpokes(ctx, c, engineType, spokes) {
		switch {
		case !st.Registered:
			findings = append(findings, diag.Finding{Code: diag.CodeSpokeUnreachable, Severity: diag.SeverityWarning,
				Message:     fmt.Sprintf("spoke %q is declared but not registered (hub secret cube-idp-spoke-%s missing)", st.Name, st.Name),
				Remediation: "run `cube-idp up` to bootstrap and register the spoke"})
		case !st.Reachable:
			rem := "check the spoke cluster and the network path to it; `cube-idp up` re-issues credentials"
			if st.Provider == "kind" {
				rem = fmt.Sprintf("kind spokes register a docker-network-internal URL for the hub engine — verify the spoke itself with: kubectl --context kind-<cube>-spoke-%s get ns", st.Name)
			}
			findings = append(findings, diag.Finding{Code: diag.CodeSpokeUnreachable, Severity: diag.SeverityWarning,
				Message:     fmt.Sprintf("spoke %q did not answer /readyz from this machine", st.Name),
				Remediation: rem})
		}
	}
	return findings
}

// ——— the tri-state checklist registry ———

// diskMinBytes is the free-space floor the disk-space check wants at the
// cube-idp config/cache dir (kind node images are the dominant consumer) —
// the same 5 GiB the doctor command passed inline before U5.
const diskMinBytes = 5 << 30

// Check is one named doctor probe: every registered check renders exactly one
// checklist row (docs/adr/0029-doctor-check-reporting.md). Run returns a non-empty one-line
// detail ("what passed looks like") with no findings for a green row, or
// ("", findings) to color the row — SeverityWarning yellow, SeverityError
// red. Run returns the detail (rather than mutating a Detail field: a
// value-slice element cannot be mutated by its own closure) and a SLICE of
// findings (inotify and spoke-reachability are multi-finding — a single
// pointer would drop entries from the documented findings array); the U5
// ledger records both deviations from the plan sketch.
type Check struct {
	Name string // stable id, e.g. "container-runtime" — part of the JSON contract
	Run  func() (string, []diag.Finding)
}

// CheckResult is one executed Check: the material of one checklist row.
type CheckResult struct {
	Name     string
	Detail   string         // green rows: what passed looks like; else ""
	Findings []diag.Finding // empty on green
}

// Status returns the row verdict: "ok" (no findings), "warn", or "fail"
// (any error finding — the exit-1 driver: doctor exits 1 iff any row is red).
// Any finding at all
// forfeits green; none of today's checks emits SeverityInfo.
func (r CheckResult) Status() string {
	s := "ok"
	for _, f := range r.Findings {
		if f.Severity == diag.SeverityError {
			return "fail"
		}
		s = "warn"
	}
	return s
}

// Worst returns the finding that colors the row — the first error, else
// the first finding; nil on green.
func (r CheckResult) Worst() *diag.Finding {
	for i := range r.Findings {
		if r.Findings[i].Severity == diag.SeverityError {
			return &r.Findings[i]
		}
	}
	if len(r.Findings) == 0 {
		return nil
	}
	return &r.Findings[0]
}

// RunChecks executes checks in registration order, one result per check.
func RunChecks(checks []Check) []CheckResult {
	results := make([]CheckResult, 0, len(checks))
	for _, c := range checks {
		detail, findings := c.Run()
		results = append(results, CheckResult{Name: c.Name, Detail: detail, Findings: findings})
	}
	return results
}

// one lifts a single optional finding into Run's slice form.
func one(f *diag.Finding) []diag.Finding {
	if f == nil {
		return nil
	}
	return []diag.Finding{*f}
}

// All assembles the host-side tri-state checklist for this cube,
// each entry wrapping its existing Check* func unchanged: container-runtime
// (kind clusters — the provider gate the doctor command always applied),
// gateway-port (and http-port when the opt-in spec.gateway.httpPort is
// set), disk-space, inotify (linux hosts), git-cli. A check that cannot
// apply to this cube/host is not registered — a row always means "this was
// probed now"; vacuous passes that WERE probed (git-cli with no
// git-sourced refs) stay registered and say so in their detail.
// spoke-reachability is not assembled here: it needs a live cluster
// client, so the doctor command appends it when a connection exists.
func All(cube *config.Cube, clusterExists bool) []Check {
	var checks []Check
	if cube.Spec.Cluster.Provider == "kind" {
		checks = append(checks, Check{Name: "container-runtime", Run: func() (string, []diag.Finding) {
			if f := CheckRuntime(); f != nil {
				return "", one(f)
			}
			for _, bin := range runtimeBins {
				if _, err := exec.LookPath(bin); err == nil {
					return bin + " on PATH", nil
				}
			}
			return "container runtime on PATH", nil
		}})
	}
	gw := cube.Spec.Gateway
	checks = append(checks, Check{Name: "gateway-port", Run: func() (string, []diag.Finding) {
		if f := CheckPortFree(gw.Port, clusterExists); f != nil {
			return "", one(f)
		}
		return fmt.Sprintf("port %d free", gw.Port), nil
	}})
	if gw.HTTPPort > 0 {
		checks = append(checks, Check{Name: "http-port", Run: func() (string, []diag.Finding) {
			if f := CheckHostPortFree(gw.HTTPPort, clusterExists, "spec.gateway.httpPort"); f != nil {
				return "", one(f)
			}
			return fmt.Sprintf("port %d free", gw.HTTPPort), nil
		}})
	}
	if dir, err := trust.Dir(); err == nil {
		checks = append(checks, Check{Name: "disk-space", Run: func() (string, []diag.Finding) {
			if f := CheckDiskSpace(dir, diskMinBytes); f != nil {
				return "", one(f)
			}
			return fmt.Sprintf("≥ %d GiB free at %s", diskMinBytes>>30, dir), nil
		}})
	}
	if goruntime.GOOS == "linux" {
		checks = append(checks, Check{Name: "inotify", Run: func() (string, []diag.Finding) {
			if fs := CheckInotify(); len(fs) > 0 {
				return "", fs
			}
			return "inotify limits at or above kind's needs", nil
		}})
	}
	checks = append(checks, Check{Name: "git-cli", Run: func() (string, []diag.Finding) {
		refs := packRefsToFetch(cube)
		if f := CheckGitCLI(refs); f != nil {
			return "", one(f)
		}
		for _, r := range refs {
			if pack.NeedsGitCLI(r) {
				return "git CLI on PATH", nil
			}
		}
		return "no git-sourced pack refs — git not needed", nil
	}})
	return checks
}

// packRefsToFetch lists every ref `up` would fetch: spec.packs plus the
// gateway pack (its ref override may also be a git source) — the same scan
// the doctor command performed inline before U5.
func packRefsToFetch(cube *config.Cube) []string {
	refs := make([]string, 0, len(cube.Spec.Packs)+1)
	for _, p := range cube.Spec.Packs {
		refs = append(refs, p.Ref)
	}
	return append(refs, cube.Spec.Gateway.PackRef())
}
