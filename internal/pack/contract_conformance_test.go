package pack

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// Contract v1 mechanical clauses (docs/pack-contract-v1.md §2): the name
// pattern and the semver version shape.
var (
	contractNameRE    = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,30}$`)
	contractVersionRE = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$`)
)

// TestReposPacksSatisfyContractV1 walks the repo's packs/ tree and
// enforces docs/pack-contract-v1.md mechanically: every pack loads, has
// name==dir matching the contract pattern, semver version, and (v1) a
// non-empty description. This test moves to $PACKS with the packs in P4 —
// P3's harness runs it there.
func TestReposPacksSatisfyContractV1(t *testing.T) {
	root := filepath.Join("..", "..", "packs")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Skipf("no packs/ tree at %s (post-P4 layout): %v", root, err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(root, e.Name())
		p, err := loadMeta(dir) // the real single-pack loader (pack.go); Fetch wraps it with ref resolution
		if err != nil {
			t.Errorf("%s: does not load: %v", e.Name(), err)
			continue
		}
		if p.Name != e.Name() {
			t.Errorf("%s: pack.cue name %q != directory", e.Name(), p.Name)
		}
		if !contractNameRE.MatchString(p.Name) {
			t.Errorf("%s: name %q does not match contract pattern %s", e.Name(), p.Name, contractNameRE)
		}
		if !contractVersionRE.MatchString(p.Version) {
			t.Errorf("%s: version %q is not semver", e.Name(), p.Version)
		}
		if p.Description == "" {
			t.Errorf("%s: contract v1 requires description", e.Name())
		}
	}
}
