// Package plugin implements cube-idp's tier-2 extensibility (spec §4.4): any
// cube-idp-<name> binary on $PATH or in InstallDir() is discoverable as
// `cube-idp <name>` — the krew model. Discovery never executes anything;
// only Exec does, and only after the trust store (trust.go) approves.
package plugin

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// pluginPrefix is the binary naming convention every discoverable plugin
// must follow: `cube-idp-<name>` becomes `cube-idp <name>`.
const pluginPrefix = "cube-idp-"

// InstallDir is Task 9's `plugin install` target and Lookup/List's second
// search location (after $PATH): $XDG_DATA_HOME/cube-idp/plugins, falling
// back to ~/.local/share/cube-idp/plugins via os.UserHomeDir when
// $XDG_DATA_HOME is unset.
func InstallDir() string {
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "cube-idp", "plugins")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".local", "share", "cube-idp", "plugins")
	}
	return filepath.Join(home, ".local", "share", "cube-idp", "plugins")
}

// Lookup finds cube-idp-<name> on $PATH (exec.LookPath semantics: must
// exist and be executable) or, failing that, in InstallDir(). Returns the
// absolute path (PATH entries in $PATH are typically already absolute;
// InstallDir() always is).
func Lookup(name string) (path string, found bool) {
	binName := pluginPrefix + name
	if p, err := exec.LookPath(binName); err == nil {
		return p, true
	}
	p := filepath.Join(InstallDir(), binName)
	if info, err := os.Stat(p); err == nil && !info.IsDir() && isExecutable(info) {
		return p, true
	}
	return "", false
}

// Descriptor is one discovered plugin.
type Descriptor struct {
	Name, Path string
	Trusted    bool
}

// List returns every discovered plugin across $PATH and InstallDir(), one
// entry per unique binary name — a $PATH entry shadows an InstallDir()
// binary of the same name, matching Lookup's own search order. Sorted by
// Name for stable output.
func List() []Descriptor {
	seen := map[string]string{} // binary name -> absolute path, first (highest-priority) hit wins
	dirs := filepath.SplitList(os.Getenv("PATH"))
	dirs = append(dirs, InstallDir())
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasPrefix(e.Name(), pluginPrefix) {
				continue
			}
			if _, exists := seen[e.Name()]; exists {
				continue
			}
			info, err := e.Info()
			if err != nil || !isExecutable(info) {
				continue
			}
			seen[e.Name()] = filepath.Join(dir, e.Name())
		}
	}

	names := make([]string, 0, len(seen))
	for n := range seen {
		names = append(names, n)
	}
	sort.Strings(names)

	descs := make([]Descriptor, 0, len(names))
	for _, n := range names {
		path := seen[n]
		descs = append(descs, Descriptor{
			Name:    strings.TrimPrefix(n, pluginPrefix),
			Path:    path,
			Trusted: isTrusted(path),
		})
	}
	return descs
}

// isExecutable reports whether info's mode grants execute permission to
// someone. On Windows there is no analogous permission bit; any regular
// file with the right name/location counts.
func isExecutable(info os.FileInfo) bool {
	if runtime.GOOS == "windows" {
		return true
	}
	return info.Mode()&0o111 != 0
}
