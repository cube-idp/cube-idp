package plugin

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/rafpe/cube-idp/internal/diag"
)

// storeDir returns (creating if needed) os.UserConfigDir()/cube-idp — the
// same directory internal/trust uses for the CA, but a private file within
// it (trust.json) so the two trust concerns (CA material vs. plugin
// binaries) never collide.
func storeDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", diag.Wrap(err, diag.CodePluginTrustIO, "cannot locate the user config directory",
			"set $HOME (or %AppData% on Windows)")
	}
	dir := filepath.Join(base, "cube-idp")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", diag.Wrap(err, diag.CodePluginTrustIO, "cannot create "+dir, "check permissions on your config directory")
	}
	return dir, nil
}

// storePath returns ~/.config/cube-idp/trust.json (or the OS equivalent).
func storePath() (string, error) {
	dir, err := storeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "trust.json"), nil
}

// loadStore reads the trust store: plugin absolute path -> trusted sha256.
// A missing file is an empty, not-yet-trusted store, not an error.
func loadStore() (map[string]string, error) {
	path, err := storePath()
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, diag.Wrap(err, diag.CodePluginTrustIO, "cannot read "+path, "check permissions on your config directory")
	}
	var m map[string]string
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, diag.Wrap(err, diag.CodePluginTrustIO, path+" is corrupt",
			"delete it to reset the plugin trust store (every plugin will be re-prompted once)")
	}
	return m, nil
}

func saveStore(m map[string]string) error {
	path, err := storePath()
	if err != nil {
		return err
	}
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return diag.Wrap(err, diag.CodePluginTrustIO, "cannot encode the plugin trust store", "retry")
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return diag.Wrap(err, diag.CodePluginTrustIO, "cannot write "+path, "check permissions on your config directory")
	}
	return nil
}

// sha256File hashes path's current contents.
func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", diag.Wrap(err, diag.CodePluginTrustIO, "cannot read "+path, "check that the plugin binary exists and is readable")
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", diag.Wrap(err, diag.CodePluginTrustIO, "cannot hash "+path, "check that the plugin binary is readable")
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Trust records path's current sha256 in the trust store unconditionally —
// used by `cube-idp plugin trust` and Task 9's verified installs.
func Trust(name, path string) error {
	sum, err := sha256File(path)
	if err != nil {
		return err
	}
	m, err := loadStore()
	if err != nil {
		return err
	}
	m[path] = sum
	return saveStore(m)
}

// isTrusted is List's read-only query: never prompts, never errors — a
// missing/corrupt store or an unreadable binary just means "not trusted".
func isTrusted(path string) bool {
	m, err := loadStore()
	if err != nil {
		return false
	}
	want, ok := m[path]
	if !ok {
		return false
	}
	sum, err := sha256File(path)
	return err == nil && sum == want
}

// EnsureTrusted enforces the trust contract for path: a known, matching
// sha256 passes silently. An unknown or CHANGED hash (an updated binary)
// re-requires trust — prompted interactively on stderr (default no) when
// interactive is true, else refused with CUBE-7104.
func EnsureTrusted(name, path string, interactive bool) error {
	m, err := loadStore()
	if err != nil {
		return err
	}
	sum, err := sha256File(path)
	if err != nil {
		return err
	}
	if want, ok := m[path]; ok && want == sum {
		return nil
	}

	remediation := fmt.Sprintf("run `cube-idp plugin trust %s`", name)
	if !interactive {
		return diag.New(diag.CodePluginUntrusted,
			fmt.Sprintf("plugin %q (%s) is not trusted", name, path), remediation)
	}

	fmt.Fprintf(os.Stderr, "! plugin %q (%s) is not trusted yet.\n", name, path)
	fmt.Fprintln(os.Stderr, "  cube-idp plugins run with your full user permissions.")
	fmt.Fprintf(os.Stderr, "  sha256: %s\n", shortSum(sum))
	fmt.Fprint(os.Stderr, "  Run it and remember this hash? [y/N] ")
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	if strings.ToLower(strings.TrimSpace(line)) != "y" {
		return diag.New(diag.CodePluginUntrusted,
			fmt.Sprintf("plugin %q (%s) was not trusted", name, path), remediation)
	}
	m[path] = sum
	return saveStore(m)
}

// shortSum truncates a sha256 hex digest for the human-facing prompt, e.g.
// "3b1f2a9c…" — the full hash is always the one written to trust.json.
func shortSum(sum string) string {
	if len(sum) <= 8 {
		return sum
	}
	return sum[:8] + "…"
}
