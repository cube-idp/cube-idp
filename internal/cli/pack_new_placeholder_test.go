package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// placeholderWarning is the line pack new prints when it wrote a chart url it
// could not know. Matched on the distinctive phrase rather than the whole
// sentence, so a wording change does not fail these tests for the wrong reason.
const placeholderWarning = "replace the placeholder chart url in "

// Both helm scaffold paths say on stdout that they wrote a placeholder url;
// the paths that write no placeholder stay quiet.
//
// The scaffold writes the same placeholder whichever path reached it
// (internal/pack/new_chart.go) and renders it at exit 0, so an operator who
// never learns of it finds out from a HelmRelease that cannot pull its chart —
// which is the reason internal/cli/pack.go gives for warning on --from-chart.
// Filling the url in makes that consequence go away; the warning exists so the
// choice is visible. "Silent" here means no warning line, not empty output: a
// successful scaffold still prints its confirmation and its render hint.
func TestPackNewPlaceholderWarning(t *testing.T) {
	tests := []struct {
		name        string
		args        func(t *testing.T) []string
		wantWarning bool
	}{
		{
			name:        "--type helm",
			args:        func(*testing.T) []string { return []string{"--type", "helm"} },
			wantWarning: true,
		},
		{
			name:        "--from-chart",
			args:        func(t *testing.T) []string { return []string{"--from-chart", chartDir(t)} },
			wantWarning: true,
		},
		{
			name: "--type raw",
			args: func(*testing.T) []string { return []string{"--type", "raw"} },
		},
		{
			name: "--type kustomize",
			args: func(*testing.T) []string { return []string{"--type", "kustomize"} },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := targetDir(t, "hello")

			code, stdout, stderr := run(t, append([]string{"pack", "new", dir}, tt.args(t)...)...)
			if code != 0 {
				t.Fatalf("exit = %d, want 0 (stderr: %s)", code, stderr)
			}
			if stderr != "" {
				t.Errorf("stderr = %q, want the warning on stdout only", stderr)
			}
			assertPlaceholderWarning(t, stdout, tt.wantWarning)
		})
	}
}

// A fork warns about nothing: the placeholder in a forked pack is its source's,
// not something this command wrote. That is a choice about what the command
// speaks for, not something the copy semantics in docs/domains/pack.md imply —
// so it gets a row of its own, with a fixture that provably still carries the
// placeholder. Without that check the row would pass for the wrong reason.
func TestPackNewForkDoesNotWarnAboutItsSourcePlaceholder(t *testing.T) {
	source := targetDir(t, "source")
	if code, _, stderr := run(t, "pack", "new", source, "--type", "helm"); code != 0 {
		t.Fatalf("scaffolding the fork source: exit = %d (stderr: %s)", code, stderr)
	}
	metadata, err := os.ReadFile(filepath.Join(source, "pack.cue"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(metadata), "REPLACE-ME") {
		t.Fatalf("the fork source no longer carries the placeholder url, so this test proves nothing:\n%s", metadata)
	}

	// --from takes a reference, not a bare path (internal/ref): an absolute
	// local source is spelled file:///abs/path.
	code, stdout, stderr := run(t, "pack", "new", targetDir(t, "fork"), "--from", "file://"+source)
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want nothing", stderr)
	}
	assertPlaceholderWarning(t, stdout, false)
}

// assertPlaceholderWarning checks the warning's presence, its count, and its
// position: once, between the confirmation and the render hint — the slot the
// --from-chart warning already occupies.
func assertPlaceholderWarning(t *testing.T, stdout string, want bool) {
	t.Helper()

	lines := strings.Split(strings.TrimSuffix(stdout, "\n"), "\n")
	var found []int
	for i, line := range lines {
		if strings.Contains(line, placeholderWarning) {
			found = append(found, i)
		}
	}
	if !want {
		if len(found) != 0 {
			t.Errorf("stdout = %q, want no placeholder warning", stdout)
		}
		return
	}
	if len(found) != 1 {
		t.Fatalf("stdout = %q, want exactly one placeholder warning, found %d", stdout, len(found))
	}
	if len(lines) != 3 || found[0] != 1 {
		t.Errorf("stdout = %q, want the warning on line 2 of three: confirmation, warning, render hint", stdout)
	}
}
