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

// TestGitSourceWithoutGitCLIWarns covers Task 4 Step 6's binding note: a
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
