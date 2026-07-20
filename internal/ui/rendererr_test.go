package ui

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/cube-idp/cube-idp/internal/diag"
	"github.com/cube-idp/cube-idp/internal/ui/event"
)

// bufWriter collects live-program output; a plain strings.Builder would do
// but bytes.Buffer keeps parity with the other pipeline tests.
type bufWriter struct{ strings.Builder }

// stripANSI removes CSI escape sequences so a styled panel can be compared
// on content and layout alone (same helper the render package tests use).
var ansiRE = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)

func stripANSI(s string) string { return ansiRE.ReplaceAllString(s, "") }

// TestTE2_DiagBoxGolden pins the diagnosis-box anatomy:
// rounded border, ✗ + code badge, cause: line, copy-paste-safe fix: line,
// and the explain footer — the box may only advertise `cube-idp explain`
// because the command ships in the same wave. Name is normative.
func TestTE2_DiagBoxGolden(t *testing.T) {
	prev := CurrentMode()
	SetMode(ModeStyled)
	defer SetMode(prev)

	err := diag.Wrap(errors.New("registry returned 401: authentication required"),
		diag.Code("CUBE-4012"),
		`cannot pull pack "ghcr.io/cube-idp/packs/gitea:0.1.0"`,
		"cube-idp repo login ghcr.io")
	got := stripANSI(RenderError(err)) + "\n"
	want, e := os.ReadFile(filepath.Join("testdata", "te2_box.golden"))
	if e != nil {
		t.Fatalf("golden: %v", e)
	}
	if got != string(want) {
		t.Fatalf("TE-2 box drifted from golden:\n got:\n%s\nwant:\n%s", got, want)
	}
	// The fix: line's content stays unstyled even before stripping — it must
	if !strings.Contains(RenderError(err), "cube-idp repo login ghcr.io") {
		t.Fatal("remediation must appear verbatim (copy-paste safe)")
	}
}

// TestRunPipelineLiveDiagnosisAfterExit is the diagnosis-last structural
// test for the ordering guarantee: a FAILING event stream through the LIVE
// renderer must (a) return the producer's error only after the bubbletea
// program has fully exited and the terminal is released, (b) never write
// the diagnosis to stdout — it renders afterwards, via ui.RenderError, at
// main.go's single final-error print point — and (c) leak no goroutine.
func TestRunPipelineLiveDiagnosisAfterExit(t *testing.T) {
	prev := CurrentMode()
	SetMode(ModeLive) // the explicit force: live even though out is not a TTY
	defer SetMode(prev)

	wantErr := diag.New(diag.Code("CUBE-3004"),
		"timed out waiting for components", "re-run `cube-idp up` (idempotent)")

	// Warm-up run: the first tea program triggers os/signal.Notify, whose
	// process-global receiver goroutine never exits by design (in the real
	// binary main.go's signal.NotifyContext creates it before any
	// pipeline). Counting goroutines after one run isolates true per-run
	// leaks from that one-time runtime infrastructure.
	var warm bufWriter
	_ = RunPipeline(context.Background(), "up", &warm,
		func(_ context.Context, _ *Console) error { return nil })

	before := runtime.NumGoroutine()
	var out bufWriter
	err := RunPipeline(context.Background(), "up", &out,
		func(_ context.Context, con *Console) error {
			con.Start("up", "dev")
			con.Step("config", "cube %q loaded and validated", "dev")
			con.Progress("cluster", "creating kind cluster") // left open — fails below
			return wantErr
		})

	// (a) The error comes back verbatim, and only after tea.Program.Run
	// returned — RunPipeline's structure guarantees the ordering; reaching
	// this line at all means the terminal was released first.
	if !errors.Is(err, wantErr) {
		t.Fatalf("producer error must return verbatim through the live pipeline, got %v", err)
	}

	// (b) The diagnosis never reaches the run's stdout: no CUBE code, no
	// summary, no remediation — those belong to RenderError on stderr,
	// which by construction runs after this function returned.
	got := out.String()
	for _, banned := range []string{string(wantErr.Code), wantErr.Summary, wantErr.Remediation} {
		if strings.Contains(got, banned) {
			t.Fatalf("the diagnosis leaked into the live run's stdout (%q):\n%s", banned, got)
		}
	}

	// The post-exit render carries the full CUBE panel.
	panel := RenderError(err)
	for _, want := range []string{string(wantErr.Code), wantErr.Summary, wantErr.Remediation} {
		if !strings.Contains(panel, want) {
			t.Fatalf("RenderError after exit missing %q:\n%s", want, panel)
		}
	}

	// (c) No goroutine survives the pipeline (§4.2).
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if runtime.NumGoroutine() <= before {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("goroutines leaked across the live pipeline: before=%d after=%d", before, runtime.NumGoroutine())
}

// TestRunPipelineLiveSuccessScrollback drives a SUCCESSFUL stream through
// the live renderer on a non-TTY writer (ModeLive is the explicit-force
// escape hatch) and asserts the scrollback content survived: step lines,
// the epilogue headline, and the Access block.
func TestRunPipelineLiveSuccessScrollback(t *testing.T) {
	prev := CurrentMode()
	SetMode(ModeLive)
	defer SetMode(prev)

	var out bufWriter
	err := RunPipeline(context.Background(), "up", &out,
		func(_ context.Context, con *Console) error {
			con.Start("up", "dev")
			con.Step("config", "cube %q loaded and validated", "dev")
			pr := con.Progress("cluster", "creating kind cluster")
			pr.Done("kind cluster ready (context kind-dev)")
			con.Epilogue(event.Epilogue{Cube: "dev", GatewayURL: "https://cube.local:8443",
				Hint: "credentials: cube-idp get secrets"})
			con.Access([]event.PackAccess{{Name: "gitea", URLs: []string{"https://gitea.cube.local:8443"}}},
				"credentials: cube-idp get secrets")
			return nil
		})
	if err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{
		"[config]", "cube \"dev\" loaded and validated",
		"[cluster]", "kind cluster ready (context kind-dev)",
		"https://cube.local:8443",
		"Access", "https://gitea.cube.local:8443",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("live scrollback missing %q:\n%q", want, got)
		}
	}
}

// TestRenderErrorPlainVerbatim pins the §5.2 contract: in ModePlain and
// ModeJSON, RenderError is diag.Render byte-for-byte — the pre-14b stderr
// block, unchanged.
func TestRenderErrorPlainVerbatim(t *testing.T) {
	prev := CurrentMode()
	defer SetMode(prev)

	err := diag.Wrap(errors.New("docker not running"), diag.Code("CUBE-1001"),
		"kind cluster create failed", "start docker and re-run `cube-idp up`")
	for _, m := range []Mode{ModePlain, ModeJSON} {
		SetMode(m)
		if got, want := RenderError(err), diag.Render(err); got != want {
			t.Fatalf("RenderError under mode %v must be diag.Render verbatim:\ngot:  %q\nwant: %q", m, got, want)
		}
	}
	// Untyped errors too.
	SetMode(ModePlain)
	plainErr := errors.New("plain failure")
	if got, want := RenderError(plainErr), diag.Render(plainErr); got != want {
		t.Fatalf("untyped RenderError drifted: %q != %q", got, want)
	}
}

// TestRenderErrorStyledPanel pins the styled branch: a bordered panel
// carrying the CUBE code, summary, cause, and the remediation as
// copy-paste-safe text (present verbatim as a substring — no styling
// injected inside the remediation string itself).
func TestRenderErrorStyledPanel(t *testing.T) {
	prev := CurrentMode()
	SetMode(ModeStyled)
	defer SetMode(prev)

	err := diag.Wrap(errors.New("docker not running"), diag.Code("CUBE-1001"),
		"kind cluster create failed", "start docker and re-run `cube-idp up`")
	got := RenderError(err)
	for _, want := range []string{"CUBE-1001", "kind cluster create failed", "docker not running",
		"start docker and re-run `cube-idp up`"} {
		if !strings.Contains(got, want) {
			t.Fatalf("styled panel missing %q:\n%s", want, got)
		}
	}
	if !strings.Contains(got, "╭") { // the rounded border marks the panel shape
		t.Fatalf("styled RenderError must render a bordered panel:\n%s", got)
	}
}

// TestRenderErrorToNonTerminalStaysPlain pins the writer-aware error seam:
// even under a styled process-wide mode, RenderErrorTo for a non-terminal
// writer (every `2>file` redirect, every test buffer) is diag.Render
// byte-for-byte — zero ANSI ever lands in a redirected stderr (audit P11).
func TestRenderErrorToNonTerminalStaysPlain(t *testing.T) {
	prev := CurrentMode()
	SetMode(ModeStyled)
	defer SetMode(prev)

	err := diag.Wrap(errors.New("docker not running"), diag.Code("CUBE-1001"),
		"kind cluster create failed", "start docker and re-run `cube-idp up`")
	var buf bytes.Buffer
	if got, want := RenderErrorTo(&buf, err), diag.Render(err); got != want {
		t.Fatalf("RenderErrorTo on a non-terminal writer must be diag.Render verbatim:\ngot:  %q\nwant: %q", got, want)
	}
	if got := RenderErrorTo(&buf, err); strings.Contains(got, "\x1b[") {
		t.Fatalf("RenderErrorTo leaked ANSI into a non-terminal writer:\n%q", got)
	}
}

// TestPrinterRenderErrorFollowsOwnMode pins the seam syncer.Watch relies on
// (Fix 4): Printer.RenderError must key off the Printer's own resolved mode,
// not the process-wide CurrentMode() the package-level RenderError uses —
// so a Printer built for a non-terminal writer (every test buffer, every
// pipe) stays plain even while CurrentMode() is styled, and a Printer built
// with an explicitly forced styled mode renders the same bordered panel
// RenderError does.
func TestPrinterRenderErrorFollowsOwnMode(t *testing.T) {
	prev := CurrentMode()
	defer SetMode(prev)

	err := diag.Wrap(errors.New("docker not running"), diag.Code("CUBE-1001"),
		"kind cluster create failed", "start docker and re-run `cube-idp up`")

	// CurrentMode() styled, but NewFor downgrades a non-terminal writer to
	// plain — Printer.RenderError must follow that downgrade, not the
	// process-wide mode.
	SetMode(ModeStyled)
	var buf bufWriter
	plain := NewFor(&buf)
	if got, want := plain.RenderError(err), diag.Render(err); got != want {
		t.Fatalf("Printer.RenderError on a non-terminal writer must stay plain:\ngot:  %q\nwant: %q", got, want)
	}

	// An explicitly styled Printer renders the identical panel the
	// package-level RenderError does under the same mode.
	styled := &Printer{out: &buf, mode: ModeStyled}
	got := styled.RenderError(err)
	want := RenderError(err) // CurrentMode() is still ModeStyled here
	if got != want {
		t.Fatalf("Printer.RenderError(styled) must match package RenderError:\ngot:  %q\nwant: %q", got, want)
	}
	if !strings.Contains(got, "╭") {
		t.Fatalf("styled Printer.RenderError must render a bordered panel:\n%s", got)
	}
}
