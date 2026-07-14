package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

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

	c := &cobra.Command{}
	var out bytes.Buffer
	c.SetOut(&out)

	if err := revertTrust(c); err != nil {
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

	c := &cobra.Command{}
	var out bytes.Buffer
	c.SetOut(&out)

	if err := revertTrust(c); err != nil {
		t.Fatalf("revertTrust must not fail down when the trust dir is unavailable: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "warning") || !strings.Contains(got, "cube-idp trust --uninstall") {
		t.Fatalf("expected warning + remediation, got:\n%s", got)
	}
}
