package cli_test

import (
	"path/filepath"
	"strings"
	"testing"
)

// The three lines `pack new` prints name the target directory the same way —
// the way the user spelled it, trailing separators aside. This is the reported
// reproduction: the
// confirmation and the render hint interpolate the argument, while the
// placeholder warning built its path with filepath.Join, whose Clean strips a
// leading "./", so one command named one directory two ways two lines apart.
//
// Not parallel and not table-driven: t.Chdir cannot run alongside parallel
// tests, and a relative argument is the whole point — an absolute one has no
// "./" to lose.
func TestPackNewNamesOneDirectoryOneWay(t *testing.T) {
	chart := chartDir(t)
	t.Chdir(t.TempDir())

	code, stdout, stderr := run(t, "pack", "new", "./podinfo", "--from-chart", chart)
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, stderr)
	}

	lines := strings.Split(strings.TrimSuffix(stdout, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("stdout = %q, want three lines", stdout)
	}
	for i, line := range lines {
		if !strings.Contains(line, "./podinfo") {
			t.Errorf("line %d = %q, want it to spell the directory ./podinfo", i+1, line)
		}
	}
	// Built rather than spelled: the separator between the directory and the
	// file is the host's, so this assertion is about the directory keeping its
	// "./" and not about "/" being the separator everywhere.
	want := "./podinfo" + string(filepath.Separator) + "pack.cue"
	if !strings.Contains(lines[1], want) {
		t.Errorf("the placeholder warning = %q, want it to name %s", lines[1], want)
	}
}
