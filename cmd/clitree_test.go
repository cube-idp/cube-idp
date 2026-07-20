package cmd

import (
	"flag"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	pflag "github.com/spf13/pflag"
)

// updateGolden regenerates cmd/testdata/clitree.golden instead of asserting
// against it. The repo had no shared golden-update convention when this fence
// was written (the TE goldens in internal/ui and cmd are hand-maintained,
// compared read-only with no -update flag), so this fence introduces the
// idiomatic Go `-update` flag for its own golden. Regenerate with:
//
//	go test ./cmd/ -run TestCommandTreeGolden -update
var updateGolden = flag.Bool("update", false, "rewrite cmd/testdata/clitree.golden from the current command tree")

// TestCommandTreeGolden is the permanent CLI-surface fence. It
// walks NewRootCmd()'s cobra tree recursively and renders one deterministic
// line per command:
//
//	<command path> | <Short> | flag=default,flag=default…
//
// Flags are the command's own non-inherited flags (root carries the three
// persistent flags; each subcommand carries only what it defines), sorted by
// name so the rendering is stable regardless of registration order. Commands
// are sorted by path. The result is compared byte-for-byte against
// cmd/testdata/clitree.golden.
//
// From this test on, ANY change to the CLI surface — a new command, a renamed
// flag, a changed default, an edited Short — must consciously regenerate the
// golden (`-update`). The CLI is frozen the way the plain projection already
// is.
func TestCommandTreeGolden(t *testing.T) {
	got := renderCommandTree(NewRootCmd())

	goldenPath := filepath.Join("testdata", "clitree.golden")
	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatalf("mkdir testdata: %v", err)
		}
		if err := os.WriteFile(goldenPath, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		t.Logf("wrote %s", goldenPath)
		return
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden (run `go test ./cmd/ -run TestCommandTreeGolden -update` to create it): %v", err)
	}
	if got != string(want) {
		t.Fatalf("command tree drifted from golden — if this is an intended CLI-surface change, regenerate with `go test ./cmd/ -run TestCommandTreeGolden -update`.\n got:\n%s\nwant:\n%s", got, want)
	}
}

// renderCommandTree walks the tree rooted at root and returns the stable
// per-command rendering described on TestCommandTreeGolden.
func renderCommandTree(root *cobra.Command) string {
	var lines []string
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		if c.Hidden {
			return
		}
		lines = append(lines, renderCommandLine(c))
		children := c.Commands()
		for _, child := range children {
			walk(child)
		}
	}
	walk(root)
	sort.Strings(lines)
	return strings.Join(lines, "\n") + "\n"
}

// renderCommandLine renders a single command as `path | Short | flags`.
func renderCommandLine(c *cobra.Command) string {
	var flags []string
	c.NonInheritedFlags().VisitAll(func(f *pflag.Flag) {
		if f.Hidden {
			return
		}
		flags = append(flags, f.Name+"="+f.DefValue)
	})
	sort.Strings(flags)
	return c.CommandPath() + " | " + c.Short + " | " + strings.Join(flags, ",")
}
