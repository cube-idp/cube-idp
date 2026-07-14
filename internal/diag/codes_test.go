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

// canonicalCodesGo is the ONLY file exempt from the CUBE-code literal ban —
// anchored to its exact repo-relative path, not just a basename match, so a
// same-named codes.go anywhere else in the tree (a different package's own
// "codes.go", say) is never accidentally exempted too.
const canonicalCodesGo = "internal/diag/codes.go"

// findCubeLiteralOffenders walks root and returns the repo-relative paths of
// every non-test .go file (other than canonicalCodesGo) containing a
// `"CUBE-` literal.
func findCubeLiteralOffenders(root string) ([]string, error) {
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
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel := strings.TrimPrefix(path, root+string(os.PathSeparator))
		if filepath.ToSlash(rel) == canonicalCodesGo {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(raw), `"CUBE-`) {
			offenders = append(offenders, rel)
		}
		return nil
	})
	return offenders, err
}

// TestNoCubeLiteralsOutsideCatalog is the debt-paydown enforcement: every
// CUBE code lives in codes.go and nowhere else in non-test Go code. Test
// files MAY use literals (asserting user-visible strings is the point of a
// golden test).
func TestNoCubeLiteralsOutsideCatalog(t *testing.T) {
	offenders, err := findCubeLiteralOffenders(repoRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(offenders) > 0 {
		t.Fatalf("CUBE-code literals outside internal/diag/codes.go — use the catalog constants:\n  %s",
			strings.Join(offenders, "\n  "))
	}
}

// TestCubeLiteralScanAnchorsToCanonicalPath is the (d) regression net: the
// exemption must key off the exact "internal/diag/codes.go" path, not just
// a basename match — a decoy codes.go elsewhere in the tree carrying a
// "CUBE- literal must still be flagged.
func TestCubeLiteralScanAnchorsToCanonicalPath(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "internal", "diag")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(real, "codes.go"), []byte(`package diag

const CodeFoo Code = "CUBE-0001"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	decoy := filepath.Join(root, "internal", "other")
	if err := os.MkdirAll(decoy, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(decoy, "codes.go"), []byte(`package other

const oops = "CUBE-9999"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	offenders, err := findCubeLiteralOffenders(root)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join("internal", "other", "codes.go")
	if len(offenders) != 1 || offenders[0] != want {
		t.Fatalf("want exactly [%s] flagged (the canonical internal/diag/codes.go must stay exempt), got %v", want, offenders)
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
