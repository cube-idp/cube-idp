package pack

import (
	"os"
	"path/filepath"

	"github.com/rafpe/cube-idp/internal/diag"
)

// GuardTree applies cube-idp's extraction guards (spec §4.4) to a fetched
// pack tree: every symlink is removed (a pack is data-only — a symlink can
// point outside the tree or alias files during render), and any walk error
// aborts the fetch. Applied to ALL getter output, regardless of source.
func GuardTree(root string) ([]string, error) {
	var removed []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink == 0 {
			return nil
		}
		if err := os.Remove(path); err != nil {
			return diag.Wrap(err, diag.CodePackGuardTrip,
				"cannot remove symlink "+path+" from the fetched pack",
				"the pack source contains symlinks cube-idp refuses to follow; re-publish the pack without them")
		}
		rel, _ := filepath.Rel(root, path)
		removed = append(removed, rel)
		return nil
	})
	if err != nil {
		if _, ok := err.(*diag.Error); ok {
			return nil, err
		}
		return nil, diag.Wrap(err, diag.CodePackGuardTrip, "cannot scan the fetched pack tree", "check permissions under the cache dir")
	}
	return removed, nil
}
