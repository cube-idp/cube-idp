package diag

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// repoRoot walks up from the package dir to the go.mod.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above " + dir)
		}
		dir = parent
	}
}

// TestNoCubeLiteralsOutsideCatalog is the debt-paydown enforcement: every
// CUBE code lives in codes.go and nowhere else in non-test Go code. Test
// files MAY use literals (asserting user-visible strings is the point of a
// golden test).
func TestNoCubeLiteralsOutsideCatalog(t *testing.T) {
	root := repoRoot(t)
	var offenders []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == "testdata" || name == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") ||
			filepath.Base(path) == "codes.go" {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(raw), `"CUBE-`) {
			offenders = append(offenders, strings.TrimPrefix(path, root+string(os.PathSeparator)))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(offenders) > 0 {
		t.Fatalf("CUBE-code literals outside internal/diag/codes.go — use the catalog constants:\n  %s",
			strings.Join(offenders, "\n  "))
	}
}

// TestCatalogWellFormed parses codes.go and asserts format + uniqueness.
func TestCatalogWellFormed(t *testing.T) {
	raw, err := os.ReadFile("codes.go")
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`Code = "(CUBE-[0-9]{4})"`)
	seen := map[string]bool{}
	matches := re.FindAllStringSubmatch(string(raw), -1)
	if len(matches) == 0 {
		t.Fatal("catalog is empty — codes.go must define every CUBE code")
	}
	for _, m := range matches {
		if seen[m[1]] {
			t.Fatalf("duplicate code %s in the catalog", m[1])
		}
		seen[m[1]] = true
	}
}
