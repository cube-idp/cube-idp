package cmd

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rafpe/cube-idp/internal/trust"
	"github.com/rafpe/cube-idp/internal/ui"
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
