package doctor

import (
	"fmt"
	"math"
	"net"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rafpe/cube-idp/internal/diag"
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
// git-sourced pack ref without the git CLI on PATH must surface a CUBE-0101
// warning naming the real cause (pack fetch would otherwise fail deep
// inside go-getter with a much less legible error).
func TestGitSourceWithoutGitCLIWarns(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir) // no git anywhere
	f := CheckGitCLI([]string{"github.com/org/repo@v1.0.0"})
	if f == nil || f.Code != "CUBE-0101" || f.Severity != diag.SeverityWarning {
		t.Fatalf("want CUBE-0101 warning, got %+v", f)
	}
	if !strings.Contains(f.Message, "git") {
		t.Fatalf("message must mention git: %+v", f)
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
