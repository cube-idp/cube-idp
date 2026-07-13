package pack

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/rafpe/cube-idp/internal/diag"
)

// DefaultCacheDir returns (creating it if needed) the local pack cache
// directory, $HOME/.cache/cube-idp/packs. Shared by `up` and `diff` so both
// resolve the same on-disk pack fetches.
func DefaultCacheDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", diag.Wrap(err, diag.CodePackCacheDirErr, "cannot determine home directory for the pack cache",
			"set $HOME, or check your environment")
	}
	dir := filepath.Join(home, ".cache", "cube-idp", "packs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", diag.Wrap(err, diag.CodePackCacheDirErr, fmt.Sprintf("cannot create pack cache dir %s", dir),
			"check permissions on $HOME/.cache")
	}
	return dir, nil
}
