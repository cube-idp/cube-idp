package cmd

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/cube-idp/cube-idp/internal/config"
	"github.com/cube-idp/cube-idp/internal/diag"
	"github.com/cube-idp/cube-idp/internal/trust"
	"github.com/cube-idp/cube-idp/internal/ui"
)

// runRevertTrust wraps revertTrust in the Task 14b event pipeline exactly
// the way cmd/down.go's RunE does — a bytes.Buffer always projects plain,
// so every substring assertion below sees the same bytes a piped `down`
// run prints. Only this call plumbing changed with 14b; the assertions are
// byte-for-byte the pre-14b ones.
func runRevertTrust(out *bytes.Buffer) error {
	return ui.RunPipeline(context.Background(), "down", out,
		func(_ context.Context, con *ui.Console) error { return revertTrust(con) })
}

// TestRevertTrustWarnsOnCorruptState covers CUBE-6006: a corrupt
// trust-state.yaml must not fail `down` (deletion already succeeded by the
// time revertTrust runs) but must surface a clear warning + manual
// remediation instead of silently skipping the revert.
func TestRevertTrustWarnsOnCorruptState(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "trust-state.yaml"), []byte("{{{"), 0o644); err != nil {
		t.Fatal(err)
	}

	restore := trustDir
	trustDir = func() (string, error) { return dir, nil }
	defer func() { trustDir = restore }()

	var out bytes.Buffer

	if err := runRevertTrust(&out); err != nil {
		t.Fatalf("revertTrust must not fail down on a corrupt state file: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "warning") {
		t.Fatalf("expected a warning about the unreadable trust state, got:\n%s", got)
	}
	if !strings.Contains(got, "cube-idp trust --uninstall") {
		t.Fatalf("expected manual remediation guidance, got:\n%s", got)
	}
}

// TestRevertTrustDirErrorWarns covers the case where the trust dir itself
// cannot be resolved/created — same contract: warn, don't fail.
func TestRevertTrustDirErrorWarns(t *testing.T) {
	restore := trustDir
	trustDir = func() (string, error) { return "", os.ErrPermission }
	defer func() { trustDir = restore }()

	var out bytes.Buffer

	if err := runRevertTrust(&out); err != nil {
		t.Fatalf("revertTrust must not fail down when the trust dir is unavailable: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "warning") || !strings.Contains(got, "cube-idp trust --uninstall") {
		t.Fatalf("expected warning + remediation, got:\n%s", got)
	}
}

// TestRevertTrustUninstallsWhenInstalled covers the happy path: a state file
// recording Installed:true must trigger trustUninstall and report the
// revert (D6: `down` always undoes what `trust` did).
func TestRevertTrustUninstallsWhenInstalled(t *testing.T) {
	dir := t.TempDir()
	if err := trust.SaveState(dir, &trust.State{Installed: true, CACert: "irrelevant"}); err != nil {
		t.Fatal(err)
	}

	restoreDir := trustDir
	trustDir = func() (string, error) { return dir, nil }
	defer func() { trustDir = restoreDir }()

	uninstalled := false
	restoreUninstall := trustUninstall
	trustUninstall = func(d string) error { uninstalled = true; return nil }
	defer func() { trustUninstall = restoreUninstall }()

	var out bytes.Buffer

	if err := runRevertTrust(&out); err != nil {
		t.Fatalf("revertTrust must not fail: %v", err)
	}
	if !uninstalled {
		t.Fatal("revertTrust must call trustUninstall when the state says Installed:true")
	}
	if !strings.Contains(out.String(), "reverted") {
		t.Fatalf("expected a reverted notice, got:\n%s", out.String())
	}
}

// TestRevertTrustNoOpWhenNotInstalled covers the common case: `trust` was
// never run, so `down` must not touch the OS trust store or print anything.
func TestRevertTrustNoOpWhenNotInstalled(t *testing.T) {
	dir := t.TempDir() // no trust-state.yaml written — LoadState defaults Installed:false

	restoreDir := trustDir
	trustDir = func() (string, error) { return dir, nil }
	defer func() { trustDir = restoreDir }()

	uninstalled := false
	restoreUninstall := trustUninstall
	trustUninstall = func(d string) error { uninstalled = true; return nil }
	defer func() { trustUninstall = restoreUninstall }()

	var out bytes.Buffer

	if err := runRevertTrust(&out); err != nil {
		t.Fatalf("revertTrust must not fail: %v", err)
	}
	if uninstalled {
		t.Fatal("revertTrust must not call trustUninstall when nothing was ever installed")
	}
	if out.String() != "" {
		t.Fatalf("expected no output for the no-op case, got:\n%s", out.String())
	}
}

// TestRevertTrustPropagatesUninstallError covers CUBE-6003 propagating: once
// the state says Installed:true, a failing trustUninstall must fail `down`
// (unlike the corrupt-state/dir-error cases, which are recoverable-unknown
// states, not a known, unreverted installation).
func TestRevertTrustPropagatesUninstallError(t *testing.T) {
	dir := t.TempDir()
	if err := trust.SaveState(dir, &trust.State{Installed: true}); err != nil {
		t.Fatal(err)
	}

	restoreDir := trustDir
	trustDir = func() (string, error) { return dir, nil }
	defer func() { trustDir = restoreDir }()

	restoreUninstall := trustUninstall
	trustUninstall = func(d string) error { return errors.New("boom") }
	defer func() { trustUninstall = restoreUninstall }()

	var out bytes.Buffer

	if err := runRevertTrust(&out); err == nil {
		t.Fatal("expected trustUninstall's error to propagate")
	}
}

// cubeYAMLFixture arranges a valid cube.yaml the way this package's other
// tests do (init into a fresh working directory) and returns its path. The
// cube name deliberately matches NO real cluster on any machine: a `--yes`
// run reaches the pipeline, and kind's Delete must be a guaranteed no-op.
func cubeYAMLFixture(t *testing.T) string {
	t.Helper()
	t.Chdir(t.TempDir())
	root := NewRootCmd()
	root.SetOut(&bytes.Buffer{})
	root.SetArgs([]string{"init", "--name", "te3-down-fixture"})
	if err := root.Execute(); err != nil {
		t.Fatalf("init fixture: %v", err)
	}
	return "cube.yaml"
}

// stubTrustSeams keeps every TE-3 test away from the developer's real OS
// trust store: down's revertTrust tail reads trustDir/trustUninstall, and a
// test that reaches the pipeline must see an empty (not-installed) state.
func stubTrustSeams(t *testing.T) {
	t.Helper()
	dir := t.TempDir() // no trust-state.yaml — LoadState defaults Installed:false
	restoreDir, restoreUninstall := trustDir, trustUninstall
	trustDir = func() (string, error) { return dir, nil }
	trustUninstall = func(string) error {
		t.Error("trustUninstall must never run from a TE-3 test")
		return nil
	}
	t.Cleanup(func() { trustDir, trustUninstall = restoreDir, restoreUninstall })
}

// ansiRE / stripANSI mirror render/styled_test.go: the TE-3.1 golden pins
// content and layout; color roles stay the theme's business.
var ansiRE = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)

func stripANSI(s string) string { return ansiRE.ReplaceAllString(s, "") }

// TestTE3_DownPreviewGolden pins the TE-3.1 preview: the enumerated deletion
// set for the kind branch (golden), plus the keep-cluster and trust-installed
// variants that must surface their own real consequences.
func TestTE3_DownPreviewGolden(t *testing.T) {
	stubTrustSeams(t) // empty state — no trust bullet in the golden
	cube := &config.Cube{}
	cube.Metadata.Name = "voodoo"
	cube.Spec.Cluster.Provider = "kind"
	cube.Spec.Engine.Type = "flux"
	cube.Spec.Packs = make([]config.PackRef, 7)

	var out bytes.Buffer
	printDownPreview(&out, cube, false)
	got := stripANSI(out.String())
	want, err := os.ReadFile(filepath.Join("testdata", "te3_preview.golden"))
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Fatalf("TE-3.1 preview drifted from golden.\ngot:\n%s\nwant:\n%s", got, want)
	}

	// keep-cluster branch mirrors runDown's engine/cascade path, not a
	// cluster delete.
	out.Reset()
	printDownPreview(&out, cube, true)
	kept := stripANSI(out.String())
	if !strings.Contains(kept, "cluster kept") || strings.Contains(kept, "kind cluster +") {
		t.Fatalf("keep-cluster preview must describe the cascade path, got:\n%s", kept)
	}

	// The OS trust-store bullet appears only when the state says Installed.
	dir := t.TempDir()
	if err := trust.SaveState(dir, &trust.State{Installed: true}); err != nil {
		t.Fatal(err)
	}
	restore := trustDir
	trustDir = func() (string, error) { return dir, nil }
	defer func() { trustDir = restore }()
	out.Reset()
	printDownPreview(&out, cube, false)
	if !strings.Contains(stripANSI(out.String()), "OS trust-store entry") {
		t.Fatalf("installed trust state must add the trust-store bullet, got:\n%s", out.String())
	}
}

// R3 (spec §5 + TE-3.4): non-TTY down without --yes REFUSES — it must
// never destroy silently in CI, and must never hang waiting for input.
func TestTE3_NonTTYRefusesWithoutYes(t *testing.T) {
	stubTrustSeams(t)
	root := NewRootCmd()
	root.SetArgs([]string{"down", "-f", cubeYAMLFixture(t)})
	root.SetIn(&bytes.Buffer{})
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	err := root.ExecuteContext(context.Background())
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != diag.CodeConfirmRequired {
		t.Fatalf("want CUBE-0010 refusal, got %v", err)
	}
}

// --yes is the scriptable twin: it must skip the consent gate entirely. The
// run may still fail later for cluster reasons — assert only that the error
// is not the CUBE-0010 refusal.
func TestTE3_YesSkipsPrompt(t *testing.T) {
	stubTrustSeams(t)
	root := NewRootCmd()
	root.SetArgs([]string{"down", "-f", cubeYAMLFixture(t), "--yes"})
	root.SetIn(&bytes.Buffer{})
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	err := root.ExecuteContext(context.Background())
	var de *diag.Error
	if errors.As(err, &de) && de.Code == diag.CodeConfirmRequired {
		t.Fatalf("--yes must skip the consent gate, still got CUBE-0010: %v", err)
	}
}

// Decline path (TE-3.3) — prompting needs a TTY, so down.go exposes seams
// (the trust.go trustInstall pattern): `var downPromptsAllowed = ui.PromptsAllowed`
// and `var downConfirmName = ui.InputExact`. Override both here: allowed=true,
// InputExact returns (false, nil) → exact wording, nil error, no pipeline run.
func TestTE3_DeclineAbortsCleanly(t *testing.T) {
	stubTrustSeams(t)
	downPromptsAllowed = func(io.Reader, io.Writer) bool { return true }
	downConfirmName = func(_ io.Reader, _ io.Writer, _, _ string) (bool, error) { return false, nil }
	defer func() { downPromptsAllowed = ui.PromptsAllowed; downConfirmName = ui.InputExact }()
	root := NewRootCmd()
	root.SetArgs([]string{"down", "-f", cubeYAMLFixture(t)})
	root.SetIn(&bytes.Buffer{})
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("decline must abort cleanly, got %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "aborted — nothing was changed") {
		t.Fatalf("want trust.go's exact abort wording, got:\n%s", got)
	}
	if strings.Contains(got, "cluster deleted") {
		t.Fatalf("decline must not run the pipeline, got:\n%s", got)
	}
}
