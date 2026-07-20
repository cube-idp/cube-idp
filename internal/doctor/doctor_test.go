package doctor

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/cube-idp/cube-idp/internal/config"
	"github.com/cube-idp/cube-idp/internal/diag"
	"github.com/cube-idp/cube-idp/internal/spoke"
	"github.com/cube-idp/cube-idp/internal/ui"
)

func TestPortSquatIsDetected(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	port := l.Addr().(*net.TCPAddr).Port

	f := CheckPortFree(port, false)
	if f == nil || f.Code != "CUBE-0102" || f.Severity != diag.SeverityError {
		t.Fatalf("squatted port must yield CUBE-0102 error, got %+v", f)
	}
	if !strings.Contains(f.Remediation, fmt.Sprint(port)) {
		t.Fatalf("remediation must name the port: %+v", f)
	}
	// when the cube is already up, the gateway holding the port is expected
	f = CheckPortFree(port, true)
	if f == nil || f.Severity != diag.SeverityWarning {
		t.Fatalf("existing cluster downgrades to warning, got %+v", f)
	}
}

func TestFreePortPasses(t *testing.T) {
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	if f := CheckPortFree(port, false); f != nil {
		t.Fatalf("free port must pass, got %+v", f)
	}
}

func TestMissingRuntimeIsDetected(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // no docker/podman/nerdctl anywhere
	f := CheckRuntime()
	if f == nil || f.Code != "CUBE-0101" || f.Severity != diag.SeverityError {
		t.Fatalf("want CUBE-0101 error, got %+v", f)
	}
}

func TestLowDiskIsAWarning(t *testing.T) {
	f := CheckDiskSpace(t.TempDir(), math.MaxUint64)
	if f == nil || f.Code != "CUBE-0103" || f.Severity != diag.SeverityWarning {
		t.Fatalf("want CUBE-0103 warning, got %+v", f)
	}
	if f := CheckDiskSpace(t.TempDir(), 1); f != nil {
		t.Fatalf("1 byte of free disk must pass, got %+v", f)
	}
}

// TestRenderPlainByteStable pins doctor's byte-frozen plain projection (design
// doc §8 item 4) — the exact "%s %s  %s\n    fix: %s\n" format, glyph as the
// bare character — survives stage B unchanged.
func TestRenderPlainByteStable(t *testing.T) {
	defer ui.SetMode(ui.ModeStyled)
	ui.SetMode(ui.ModePlain)
	var b strings.Builder
	Render(&b, []diag.Finding{
		{Code: "CUBE-0101", Severity: diag.SeverityError, Message: "no runtime", Remediation: "install docker"},
	})
	const want = "✗ CUBE-0101  no runtime\n    fix: install docker\n"
	if got := b.String(); got != want {
		t.Fatalf("doctor plain drifted:\ngot:  %q\nwant: %q", got, want)
	}
	if strings.Contains(b.String(), "\x1b[") {
		t.Fatal("plain doctor must emit zero ANSI escapes")
	}
}

// TestRenderStyledGroupsBySeverity checks the stage-B rich render (design doc
// §10): ModeLive forces styled even on a bytes.Buffer (the NewFor escape
// hatch), and the output groups findings under severity section headers and
// prints a verdict. hasErrors must still be reported correctly in styled mode.
func TestRenderStyledGroupsBySeverity(t *testing.T) {
	defer ui.SetMode(ui.ModeStyled)
	ui.SetMode(ui.ModeLive) // NewFor maps ModeLive -> styled regardless of writer
	var b strings.Builder
	hasErrors := Render(&b, []diag.Finding{
		{Code: "CUBE-0103", Severity: diag.SeverityWarning, Message: "low disk", Remediation: "free space"},
		{Code: "CUBE-0101", Severity: diag.SeverityError, Message: "no runtime", Remediation: "install docker"},
	})
	if !hasErrors {
		t.Fatal("styled render must still report errors")
	}
	got := b.String()
	for _, want := range []string{"Errors", "Warnings", "CUBE-0101", "CUBE-0103", "fix:"} {
		if !strings.Contains(got, want) {
			t.Fatalf("styled doctor missing %q:\n%s", want, got)
		}
	}
}

func TestRenderSeparatesErrorsFromWarnings(t *testing.T) {
	var b strings.Builder
	hasErrors := Render(&b, []diag.Finding{
		{Code: "CUBE-0103", Severity: diag.SeverityWarning, Message: "low disk", Remediation: "free space"},
	})
	if hasErrors {
		t.Fatal("warnings alone must not flag errors")
	}
	hasErrors = Render(&b, []diag.Finding{
		{Code: "CUBE-0101", Severity: diag.SeverityError, Message: "no runtime", Remediation: "install docker"},
	})
	if !hasErrors {
		t.Fatal("an error finding must flag errors (doctor exits 1)")
	}
}

// TestGitSourceWithoutGitCLIWarns covers the git-getter binding: bare git
// pack refs are fetched by shelling out to the git CLI, so a
// git-sourced pack ref without the git CLI on PATH must surface a CUBE-0105
// warning naming the real cause (pack fetch would otherwise fail deep
// inside go-getter with a much less legible error). A git-sourced gateway
// pack override (spec.gateway.ref) must trigger the same warning — doctor
// scans it alongside spec.packs.
func TestGitSourceWithoutGitCLIWarns(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir) // no git anywhere
	f := CheckGitCLI([]string{"github.com/org/repo@v1.0.0"})
	if f == nil || f.Code != "CUBE-0105" || f.Severity != diag.SeverityWarning {
		t.Fatalf("want CUBE-0105 warning, got %+v", f)
	}
	if !strings.Contains(f.Message, "git") {
		t.Fatalf("message must mention git: %+v", f)
	}
	// gateway override as the only git-sourced ref (spec.packs all OCI)
	f = CheckGitCLI([]string{
		"oci://ghcr.io/cube-idp/packs/gitea:0.1.0",
		"github.com/org/gateway-pack//packs/traefik@v2.0.0", // gateway PackRef()
	})
	if f == nil || f.Code != "CUBE-0105" || f.Severity != diag.SeverityWarning {
		t.Fatalf("git-sourced gateway ref must warn too, got %+v", f)
	}
}

func TestGitSourceWithGitCLIPasses(t *testing.T) {
	if f := CheckGitCLI([]string{"github.com/org/repo@v1.0.0"}); f != nil {
		t.Fatalf("git is on PATH in the test environment, want no finding, got %+v", f)
	}
}

func TestNonGitSourceNeverWarnsEvenWithoutGitCLI(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	refs := []string{"oci://ghcr.io/cube-idp/packs/gitea:0.1.0", "./packs/local", filepath.Join("packs", "traefik")}
	if f := CheckGitCLI(refs); f != nil {
		t.Fatalf("no git-sourced ref present, want no finding, got %+v", f)
	}
}

// spokeProbeServer stands in for a spoke API server: a TLS endpoint whose
// /readyz answers 200 only with the bearer token the hub secret carries
// (so a pass proves the payload's credentials actually flowed). Returns
// the server URL and the PEM CA that verifies it (the self-signed leaf is
// its own root).
func spokeProbeServer(t *testing.T) (string, []byte) {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/readyz" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		fmt.Fprint(w, "ok")
	}))
	t.Cleanup(srv.Close)
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})
	return srv.URL, ca
}

// deadEndpoint returns a URL nothing listens on (bind, read the port,
// close) so probes fail fast with connection refused.
func deadEndpoint(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	return "https://" + addr
}

func spokeFakeClient(t *testing.T, objs ...client.Object) client.Client {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
}

func fluxSpokeSecret(t *testing.T, name, server string, ca []byte) *corev1.Secret {
	t.Helper()
	kc, err := spoke.BuildKubeconfig(name, server, ca, "tok")
	if err != nil {
		t.Fatal(err)
	}
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "cube-idp-spoke-" + name, Namespace: "flux-system"},
		Data:       map[string][]byte{"value": kc},
	}
}

// TestCheckSpokeReachabilityNoSpokes: no spokes declared — the check skips
// silently (nil findings, no cluster reads beyond none).
func TestCheckSpokeReachabilityNoSpokes(t *testing.T) {
	if fs := CheckSpokeReachability(context.Background(), spokeFakeClient(t), "flux", nil); fs != nil {
		t.Fatalf("no spokes declared must yield no findings, got %+v", fs)
	}
}

// TestCheckSpokeReachabilityFluxArms exercises all three states against the
// flux payload (the `value` kubeconfig): a reachable spoke yields no
// finding, an unreachable one warns CUBE-8006 naming it, a declared-but-
// unregistered one warns CUBE-8006 naming the missing hub secret.
func TestCheckSpokeReachabilityFluxArms(t *testing.T) {
	url, ca := spokeProbeServer(t)
	c := spokeFakeClient(t,
		fluxSpokeSecret(t, "healthy", url, ca),
		fluxSpokeSecret(t, "dead", deadEndpoint(t), ca),
	)
	spokes := []config.SpokeSpec{
		{Name: "healthy", Cluster: config.ClusterSpec{Provider: "kind"}},
		{Name: "dead", Cluster: config.ClusterSpec{Provider: "existing", Context: "x"}},
		{Name: "ghost", Cluster: config.ClusterSpec{Provider: "kind"}},
	}
	fs := CheckSpokeReachability(context.Background(), c, "flux", spokes)
	if len(fs) != 2 {
		t.Fatalf("want findings for dead+ghost only, got %+v", fs)
	}
	for _, f := range fs {
		if f.Code != diag.CodeSpokeUnreachable || f.Severity != diag.SeverityWarning {
			t.Fatalf("spoke findings must be CUBE-8006 warnings, got %+v", f)
		}
	}
	if !strings.Contains(fs[0].Message, "dead") || !strings.Contains(fs[1].Message, "ghost") {
		t.Fatalf("findings must name each unreachable spoke in declaration order: %+v", fs)
	}
	if !strings.Contains(fs[1].Message, "cube-idp-spoke-ghost") {
		t.Fatalf("the unregistered arm must name the missing hub secret: %+v", fs[1])
	}
}

// TestProbeSpokesArgocdPayload: the argocd cluster-secret payload (server +
// config JSON with bearerToken and caData) rebuilds a working REST config —
// the probe authenticates with the payload's own token.
func TestProbeSpokesArgocdPayload(t *testing.T) {
	url, ca := spokeProbeServer(t)
	cc := spokeArgocdConfig{BearerToken: "tok"}
	cc.TLSClientConfig.CAData = ca
	cj, err := json.Marshal(cc)
	if err != nil {
		t.Fatal(err)
	}
	sec := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "cube-idp-spoke-prod", Namespace: "argocd"},
		Data:       map[string][]byte{"server": []byte(url), "config": cj},
	}
	states := ProbeSpokes(context.Background(), spokeFakeClient(t, sec), "argocd",
		[]config.SpokeSpec{{Name: "prod", Cluster: config.ClusterSpec{Provider: "existing", Context: "x"}}})
	if len(states) != 1 {
		t.Fatalf("want one state, got %+v", states)
	}
	s := states[0]
	if s.Name != "prod" || s.Provider != "existing" || !s.Registered || !s.Reachable {
		t.Fatalf("argocd payload must probe reachable: %+v", s)
	}
}

// TestHTTPPortProbeNamesHTTPPortField covers U2: doctor's port preflight
// also probes the opt-in gateway.httpPort, and its CUBE-0102 remediation
// blames the field the user actually set (spec.gateway.httpPort, not
// spec.gateway.port).
func TestHTTPPortProbeNamesHTTPPortField(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	port := l.Addr().(*net.TCPAddr).Port

	f := CheckHostPortFree(port, false, "spec.gateway.httpPort")
	if f == nil || f.Code != "CUBE-0102" || f.Severity != diag.SeverityError {
		t.Fatalf("squatted httpPort must yield CUBE-0102 error, got %+v", f)
	}
	if !strings.Contains(f.Remediation, "spec.gateway.httpPort") {
		t.Fatalf("remediation must name spec.gateway.httpPort: %+v", f)
	}
	// The existing single-port probe keeps blaming spec.gateway.port.
	if f := CheckPortFree(port, false); f == nil || !strings.Contains(f.Remediation, "spec.gateway.port") {
		t.Fatalf("CheckPortFree must keep naming spec.gateway.port: %+v", f)
	}
}

// checkNames projects a check set onto its stable ids (U5 test helper).
func checkNames(checks []Check) []string {
	names := make([]string, 0, len(checks))
	for _, c := range checks {
		names = append(names, c.Name)
	}
	return names
}

// TestDoctorAllAssemblesChecklist pins the check registry's assembly
// for a minimal (default-profile) cube: at least four uniquely named host
// checks, the opt-in http-port check present only when
// spec.gateway.httpPort is set, and the linux-only inotify check
// registered per GOOS. spoke-reachability is NOT here — it needs a live
// cluster client, so the doctor command appends it (All has no client).
func TestDoctorAllAssemblesChecklist(t *testing.T) {
	cube := config.Default("dev")
	checks := All(cube, false)
	if len(checks) < 4 {
		t.Fatalf("want >= 4 checks on a minimal cube, got %d: %v", len(checks), checkNames(checks))
	}
	seen := map[string]bool{}
	for _, c := range checks {
		if c.Name == "" || c.Run == nil {
			t.Fatalf("check with empty name or nil Run: %+v", checkNames(checks))
		}
		if seen[c.Name] {
			t.Fatalf("duplicate check name %q", c.Name)
		}
		seen[c.Name] = true
	}
	for _, want := range []string{"container-runtime", "gateway-port", "disk-space", "git-cli"} {
		if !seen[want] {
			t.Fatalf("check %q missing from %v", want, checkNames(checks))
		}
	}
	if seen["http-port"] {
		t.Fatalf("http-port must be opt-in — absent field, no check: %v", checkNames(checks))
	}
	if seen["inotify"] != (goruntime.GOOS == "linux") {
		t.Fatalf("inotify check registered=%v on GOOS=%s", seen["inotify"], goruntime.GOOS)
	}

	cube.Spec.Gateway.HTTPPort = 8080
	withHTTP := map[string]bool{}
	for _, c := range All(cube, false) {
		withHTTP[c.Name] = true
	}
	if !withHTTP["http-port"] {
		t.Fatalf("httpPort set must register the http-port check: %v", checkNames(All(cube, false)))
	}
}

// TestDoctorRunChecksAllGreen: a stubbed all-green run yields zero
// findings and every Detail non-empty — the whole point of the checklist:
// passes are SHOWN,
// and the detail is what the green row says.
func TestDoctorRunChecksAllGreen(t *testing.T) {
	results := RunChecks([]Check{
		{Name: "a", Run: func() (string, []diag.Finding) { return "a looks fine", nil }},
		{Name: "b", Run: func() (string, []diag.Finding) { return "b looks fine", nil }},
	})
	if len(results) != 2 {
		t.Fatalf("want one result per check, got %+v", results)
	}
	for _, r := range results {
		if len(r.Findings) != 0 {
			t.Fatalf("all-green run must yield zero findings: %+v", r)
		}
		if r.Detail == "" {
			t.Fatalf("green result must carry a non-empty Detail: %+v", r)
		}
		if r.Status() != "ok" {
			t.Fatalf("green result must be ok, got %q", r.Status())
		}
		if r.Worst() != nil {
			t.Fatalf("green result has no worst finding, got %+v", r.Worst())
		}
	}
}

// TestDoctorRunChecksSeverityFold: a multi-finding check folds to its
// worst severity for the row (error beats warning) while preserving every
// finding for the documented findings array.
func TestDoctorRunChecksSeverityFold(t *testing.T) {
	warn := diag.Finding{Code: "CUBE-0103", Severity: diag.SeverityWarning, Message: "low disk", Remediation: "free space"}
	fail := diag.Finding{Code: "CUBE-0102", Severity: diag.SeverityError, Message: "port busy", Remediation: "free the port"}
	results := RunChecks([]Check{
		{Name: "warns", Run: func() (string, []diag.Finding) { return "", []diag.Finding{warn} }},
		{Name: "fails", Run: func() (string, []diag.Finding) { return "", []diag.Finding{warn, fail} }},
	})
	if got := results[0].Status(); got != "warn" {
		t.Fatalf("warning-only check must be warn, got %q", got)
	}
	if w := results[0].Worst(); w == nil || w.Code != "CUBE-0103" {
		t.Fatalf("warn row's worst finding wrong: %+v", w)
	}
	if got := results[1].Status(); got != "fail" {
		t.Fatalf("check with an error finding must be fail, got %q", got)
	}
	if w := results[1].Worst(); w == nil || w.Code != "CUBE-0102" {
		t.Fatalf("the error finding must color the row: %+v", w)
	}
	if len(results[1].Findings) != 2 {
		t.Fatalf("fold must preserve every finding: %+v", results[1].Findings)
	}
}

// TestDoctorAllGreenDetails exercises two real wrappers end-to-end on
// their green paths: gateway-port on a known-free port names the port,
// and git-cli over the default profile's oci-only refs reports the
// vacuous pass honestly (no git-sourced refs — nothing needed git).
func TestDoctorAllGreenDetails(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	cube := config.Default("dev")
	cube.Spec.Gateway.Port = port
	for _, r := range RunChecks(All(cube, false)) {
		switch r.Name {
		case "gateway-port":
			if r.Status() != "ok" || !strings.Contains(r.Detail, fmt.Sprint(port)) {
				t.Fatalf("free gateway port must be green naming the port: %+v", r)
			}
		case "git-cli":
			if r.Status() != "ok" || !strings.Contains(r.Detail, "no git-sourced") {
				t.Fatalf("oci-only refs must be a vacuous green: %+v", r)
			}
		}
	}
}
