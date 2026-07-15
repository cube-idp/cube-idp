package diag

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
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

// cubeLiteralRe matches a CUBE literal opened by either quote character: a
// plain double-quoted string ("CUBE-...") or a raw backtick string
// (`CUBE-...`). A bare strings.Contains(`"CUBE-`) missed the backtick form
// entirely, letting a raw-string literal slip past the ban undetected.
var cubeLiteralRe = regexp.MustCompile("[\"`]CUBE-")

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
			// Skip dot-directories entirely: .git, and .claude/ where agent
			// worktrees (full repo checkouts) would otherwise be scanned.
			if strings.HasPrefix(name, ".") && path != root {
				return filepath.SkipDir
			}
			if name == "testdata" || name == "vendor" {
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
		if cubeLiteralRe.MatchString(string(raw)) {
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

// mustWriteFile creates path's parent directories and writes contents,
// failing the test on any error.
func mustWriteFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestBanCatchesBacktickLiterals: raw-string CUBE literals must be flagged
// too — the scan previously matched only "\"CUBE-".
func TestBanCatchesBacktickLiterals(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "internal", "diag", "codes.go"), "package diag\n")
	mustWriteFile(t, filepath.Join(root, "internal", "x", "x.go"),
		"package x\n\nconst oops = `CUBE-9999: raw`\n")
	offenders, err := findCubeLiteralOffenders(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(offenders) != 1 {
		t.Fatalf("backtick literal not flagged: %v", offenders)
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

// definedCode is one Code constant found in codes.go: its Go identifier, its
// CUBE-xxxx value, and whether its trailing line comment marks it reserved
// (the CUBE-3006/CodeEngineArgocdRegFail precedent: "// reserved: ...").
type definedCode struct {
	ident    string
	value    string
	reserved bool
}

// parseDefinedCodes parses codes.go with go/parser and collects every Code
// constant declared in its top-level `const ( ... )` blocks, walking each
// ast.GenDecl's ValueSpecs (the algorithm named in the brief). A constant is
// "reserved" when its trailing line comment contains the word "reserved" —
// codes.go writes these as `Code = "CUBE-xxxx" // reserved: ...`.
func parseDefinedCodes(t *testing.T) []definedCode {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "codes.go", nil, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	var out []definedCode
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			// Match each name in the spec to its positional value (codes.go
			// declares one name = one literal per line, but walk defensively).
			for i, name := range vs.Names {
				if i >= len(vs.Values) {
					continue
				}
				lit, ok := vs.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				value := strings.Trim(lit.Value, `"`)
				if !strings.HasPrefix(value, "CUBE-") {
					continue
				}
				reserved := vs.Comment != nil && strings.Contains(strings.ToLower(vs.Comment.Text()), "reserved")
				out = append(out, definedCode{ident: name.Name, value: value, reserved: reserved})
			}
		}
	}
	return out
}

// codeIdentRe matches a bare Code identifier reference: either qualified as
// diag.CodeXxx (from another package) or bare CodeXxx (within package diag
// itself, e.g. diag.go or a same-package helper). Word-boundaried so it never
// partially matches a longer identifier.
var codeIdentRe = regexp.MustCompile(`\bdiag\.(Code[A-Za-z0-9]+)\b|\b(Code[A-Za-z0-9]+)\b`)

// collectUsedCodeIdents walks root's non-test .go files (reusing
// findCubeLiteralOffenders' directory-skip rules, but WITHOUT exempting
// codes.go itself — codes.go's own declarations must not be mistaken for
// uses) and collects every distinct Code identifier referenced.
func collectUsedCodeIdents(t *testing.T, root string) map[string]bool {
	t.Helper()
	used := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if strings.HasPrefix(name, ".") && path != root {
				return filepath.SkipDir
			}
			if name == "testdata" || name == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel := strings.TrimPrefix(path, root+string(os.PathSeparator))
		if filepath.ToSlash(rel) == canonicalCodesGo {
			return nil // codes.go declares identifiers, it doesn't "use" them
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, m := range codeIdentRe.FindAllStringSubmatch(string(raw), -1) {
			ident := m[1]
			if ident == "" {
				ident = m[2]
			}
			used[ident] = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return used
}

// TestCatalogExhaustive: every Code constant defined in codes.go is either
// referenced by identifier somewhere in non-test Go code or carries a
// "reserved" marker in its trailing comment (CUBE-3006 precedent); and every
// diag.Code* identifier used in non-test code is defined in codes.go.
func TestCatalogExhaustive(t *testing.T) {
	defined := parseDefinedCodes(t)
	if len(defined) == 0 {
		t.Fatal("no Code constants parsed out of codes.go — parser or algorithm broke")
	}
	definedByIdent := map[string]definedCode{}
	for _, dc := range defined {
		definedByIdent[dc.ident] = dc
	}

	used := collectUsedCodeIdents(t, repoRoot(t))

	// 1. defined - used - reserved => unused, unreserved code.
	var unused []string
	for _, dc := range defined {
		if dc.reserved {
			continue
		}
		if !used[dc.ident] {
			unused = append(unused, dc.ident+" ("+dc.value+")")
		}
	}
	sort.Strings(unused)
	for _, u := range unused {
		t.Errorf("unused, unreserved code %s — use it or annotate `// reserved:`", u)
	}

	// 2. used - defined => undefined code identifier (typo, or a Code-shaped
	// identifier from an unrelated package that happens to match the regexp —
	// none currently exist, so a hit here is real).
	var undefined []string
	for ident := range used {
		if _, ok := definedByIdent[ident]; !ok {
			undefined = append(undefined, ident)
		}
	}
	sort.Strings(undefined)
	for _, u := range undefined {
		t.Errorf("undefined code identifier %s", u)
	}
}
