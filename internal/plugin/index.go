// Task 9 (spec §4.4): a plugin index is a git repository containing
// plugins/<name>.yaml descriptors, each pinning per-platform archive URLs
// by sha256. Install fetches the index with the system git (optionally
// pinned to a commit), downloads the archive for the current platform over
// HTTPS, verifies every byte before anything lands on disk, and — because
// the sha was proven, which is exactly what the trust prompt would
// establish — records the install as trusted automatically.
//
// There is deliberately NO DefaultIndex constant (RESOLVED 2026-07-14,
// Owner Decisions #8): `--index` is required until a first real plugin
// index exists; pointing a default at a repo that does not exist yet would
// be worse than requiring the flag. `plugin install` without --index fails
// with CUBE-7102 in cmd/plugin.go.
package plugin

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/cube-idp/cube-idp/internal/diag"
)

// maxArchiveBytes caps how much of a plugin archive Install will read into
// memory. Plugin archives are small (a single binary); anything past this
// is refused rather than silently truncated.
const maxArchiveBytes = 256 * 1024 * 1024 // 256 MiB

// indexHTTPClient is the client fetchArchive uses to download plugin
// archives. A var (not a const/local http.DefaultClient) so tests can shrink
// Timeout to exercise the timeout path deterministically; production always
// runs with the 60s default. cobra's ctx is otherwise un-deadlined, so
// without this an unresponsive or slow-loris archive host would hang
// `plugin install` forever.
var indexHTTPClient = &http.Client{Timeout: 60 * time.Second}

// IndexEntry is the decoded form of plugins/<name>.yaml in an index repo.
type IndexEntry struct {
	Name             string     `yaml:"name"`
	ShortDescription string     `yaml:"shortDescription"`
	Platforms        []Platform `yaml:"platforms"`
}

// Platform is one GOOS/GOARCH build of a plugin, pinned by sha256.
type Platform struct {
	OS     string `yaml:"os"`     // GOOS
	Arch   string `yaml:"arch"`   // GOARCH
	URL    string `yaml:"url"`    // .tar.gz containing the plugin binary
	SHA256 string `yaml:"sha256"` // hex digest of the archive
	Bin    string `yaml:"bin"`    // path of the binary inside the archive
}

// Install fetches indexURL (git; an optional @<commit> suffix pins the
// index itself), reads plugins/<name>.yaml, downloads the current-platform
// archive, verifies its sha256, extracts Bin into InstallDir() as
// cube-idp-<name> (0755), and records trust. Errors are CUBE-7102
// (index fetch, archive fetch, or archive verification) or CUBE-7101
// (name not present in the index).
func Install(ctx context.Context, indexURL, name string) error {
	clonePath, err := cloneIndex(ctx, indexURL)
	if err != nil {
		return err
	}
	defer os.RemoveAll(clonePath)

	entry, err := readEntry(clonePath, indexURL, name)
	if err != nil {
		return err
	}

	platform, err := selectPlatform(entry, name)
	if err != nil {
		return err
	}

	data, err := fetchArchive(ctx, name, platform)
	if err != nil {
		return err
	}

	binData, err := extractBin(name, platform, data)
	if err != nil {
		return err
	}

	installedPath, err := atomicInstall(name, binData)
	if err != nil {
		return err
	}

	return Trust(name, installedPath)
}

// indexPinPattern is the conservative charset a pinned "@<commit>" must
// match before it is ever handed to a git subprocess: it must start with an
// alphanumeric — so it can never be parsed as a git OPTION (the classic
// argument-injection → command-execution vector) — followed only by
// ref-safe characters. Full and abbreviated hex shas, branch names, and
// tags all fit; anything option-shaped ("-evil", "--upload-pack=…") does
// not.
var indexPinPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,254}$`)

// splitIndexPin splits an optional "@<commit>" pin off the end of indexURL.
// Only a trailing segment with neither "/" nor ":" is treated as a pin —
// that keeps ssh-style URLs like "git@github.com:org/repo.git" (whose only
// "@" is the user@host separator) intact while still recognizing
// "https://.../repo.git@deadbeef" or "...@main".
func splitIndexPin(indexURL string) (url, commit string) {
	idx := strings.LastIndex(indexURL, "@")
	if idx < 0 {
		return indexURL, ""
	}
	suffix := indexURL[idx+1:]
	if suffix == "" || strings.ContainsAny(suffix, ":/") {
		return indexURL, ""
	}
	return indexURL[:idx], suffix
}

// cloneIndex fetches indexURL's git repository into a fresh temp directory,
// checking out the pinned commit if one was given. The caller must remove
// the returned directory.
func cloneIndex(ctx context.Context, indexURL string) (string, error) {
	const failSummary = "cannot fetch plugin index"
	const failRemediation = "install git, check the index URL, or pass a different --index"

	if _, err := exec.LookPath("git"); err != nil {
		return "", diag.Wrap(err, diag.CodePluginTrustIO, failSummary, failRemediation)
	}

	url, commit := splitIndexPin(indexURL)
	// Argument-injection guard: neither the URL nor the pin may ever be
	// parseable as a git OPTION. An option-shaped URL (e.g.
	// "--upload-pack=…") or pin ("-evil") would otherwise be interpreted by
	// git as a flag — for --upload-pack, that is attacker-chosen command
	// execution on an install path. Validate BEFORE any git subprocess
	// runs; the "--" separators below are defense in depth.
	if strings.HasPrefix(url, "-") {
		return "", diag.New(diag.CodePluginTrustIO,
			fmt.Sprintf("invalid index URL %q: must not begin with '-'", url), failRemediation)
	}
	if commit != "" && !indexPinPattern.MatchString(commit) {
		return "", diag.New(diag.CodePluginTrustIO,
			fmt.Sprintf("invalid index pin %q: a pin must be a commit sha, tag, or branch name", commit),
			failRemediation)
	}

	tmp, err := os.MkdirTemp("", "cube-idp-index-*")
	if err != nil {
		return "", diag.Wrap(err, diag.CodePluginTrustIO, failSummary, failRemediation)
	}

	runGit := func(args ...string) error {
		cmd := exec.CommandContext(ctx, "git", args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return diag.Wrap(fmt.Errorf("%s", strings.TrimSpace(string(out))), diag.CodePluginTrustIO, failSummary, failRemediation)
		}
		return nil
	}

	if err := runGit("clone", "--depth", "1", "--", url, tmp); err != nil {
		os.RemoveAll(tmp)
		return "", err
	}
	if commit != "" {
		if err := runGit("-C", tmp, "fetch", "-q", "origin", "--", commit); err != nil {
			os.RemoveAll(tmp)
			return "", err
		}
		if err := runGit("-C", tmp, "checkout", "-q", commit, "--"); err != nil {
			os.RemoveAll(tmp)
			return "", err
		}
	}
	return tmp, nil
}

// readEntry reads and decodes plugins/<name>.yaml from clonePath.
func readEntry(clonePath, indexURL, name string) (*IndexEntry, error) {
	entryPath := filepath.Join(clonePath, "plugins", name+".yaml")
	raw, err := os.ReadFile(entryPath)
	if err != nil {
		return nil, diag.New(diag.CodePluginNotFound,
			fmt.Sprintf("plugin %q is not in index %s", name, indexURL),
			"run `git ls-tree` on the index or check the name")
	}
	var entry IndexEntry
	if err := yaml.Unmarshal(raw, &entry); err != nil {
		return nil, diag.Wrap(err, diag.CodePluginTrustIO,
			fmt.Sprintf("plugin %q descriptor is invalid", name),
			"report this to the index maintainers — plugins/<name>.yaml failed to parse")
	}
	return &entry, nil
}

// selectPlatform picks the Platform matching the running GOOS/GOARCH.
func selectPlatform(entry *IndexEntry, name string) (*Platform, error) {
	for i := range entry.Platforms {
		if entry.Platforms[i].OS == runtime.GOOS && entry.Platforms[i].Arch == runtime.GOARCH {
			return &entry.Platforms[i], nil
		}
	}
	return nil, diag.New(diag.CodePluginTrustIO,
		fmt.Sprintf("no %s/%s build of plugin %q in the index", runtime.GOOS, runtime.GOARCH, name),
		"ask the index maintainers to publish a build for your platform")
}

// fetchArchive downloads platform.URL into memory, capped at
// maxArchiveBytes, and verifies its sha256 matches platform.SHA256
// (case-insensitively) before returning the bytes.
func fetchArchive(ctx context.Context, name string, platform *Platform) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, platform.URL, nil)
	if err != nil {
		return nil, diag.Wrap(err, diag.CodePluginTrustIO,
			fmt.Sprintf("cannot download plugin archive for %q", name),
			"check the index URL")
	}
	resp, err := indexHTTPClient.Do(req)
	if err != nil {
		return nil, diag.Wrap(err, diag.CodePluginTrustIO,
			fmt.Sprintf("cannot download plugin archive for %q", name),
			"check network connectivity and the index URL")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, diag.New(diag.CodePluginTrustIO,
			fmt.Sprintf("cannot download plugin archive for %q: HTTP %d", name, resp.StatusCode),
			"check the index URL")
	}

	limited := io.LimitReader(resp.Body, maxArchiveBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, diag.Wrap(err, diag.CodePluginTrustIO,
			fmt.Sprintf("cannot download plugin archive for %q", name),
			"check network connectivity and the index URL")
	}
	if len(data) > maxArchiveBytes {
		return nil, diag.New(diag.CodePluginTrustIO,
			fmt.Sprintf("plugin archive for %q exceeds the 256 MiB limit", name),
			"report this to the index maintainers — archives must stay under 256 MiB")
	}

	sum := sha256.Sum256(data)
	got := hex.EncodeToString(sum[:])
	if !strings.EqualFold(got, platform.SHA256) {
		return nil, diag.New(diag.CodePluginTrustIO,
			fmt.Sprintf("sha256 mismatch for %s: index pins %s…, got %s…", name, shortSum(strings.ToLower(platform.SHA256)), shortSum(got)),
			"the archive changed since the index pinned it — do not install; report it to the index maintainers")
	}
	return data, nil
}

// extractBin extracts exactly platform.Bin from the tar.gz in data,
// rejecting path-traversal entry names.
func extractBin(name string, platform *Platform, data []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, diag.Wrap(err, diag.CodePluginTrustIO,
			fmt.Sprintf("plugin archive for %q is not a valid gzip stream", name),
			"report this to the index maintainers")
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	wantBin := strings.TrimPrefix(platform.Bin, "./")
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, diag.Wrap(err, diag.CodePluginTrustIO,
				fmt.Sprintf("plugin archive for %q is corrupt", name),
				"report this to the index maintainers")
		}
		entryName := strings.TrimPrefix(hdr.Name, "./")
		if filepath.IsAbs(entryName) || strings.Contains(entryName, "..") {
			continue // path-traversal guard: never even consider such an entry
		}
		if entryName != wantBin {
			continue
		}
		binData, err := io.ReadAll(tr)
		if err != nil {
			return nil, diag.Wrap(err, diag.CodePluginTrustIO,
				fmt.Sprintf("plugin archive for %q is corrupt", name),
				"report this to the index maintainers")
		}
		return binData, nil
	}
	return nil, diag.New(diag.CodePluginTrustIO,
		fmt.Sprintf("plugin archive for %q does not contain %s", name, platform.Bin),
		"report this to the index maintainers")
}

// atomicInstall writes binData to InstallDir()/cube-idp-<name> via a
// temp-file-then-rename, so a crash mid-write never leaves a half-written
// executable at the final path.
func atomicInstall(name string, binData []byte) (string, error) {
	dir := InstallDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", diag.Wrap(err, diag.CodePluginTrustIO, "cannot create "+dir, "check permissions on your plugin install directory")
	}

	finalPath := filepath.Join(dir, pluginPrefix+name)
	tmp, err := os.CreateTemp(dir, "."+pluginPrefix+name+"-*")
	if err != nil {
		return "", diag.Wrap(err, diag.CodePluginTrustIO, "cannot create a temp file in "+dir, "check permissions on your plugin install directory")
	}
	tmpPath := tmp.Name()
	// Best-effort cleanup: once the rename below succeeds, tmpPath no
	// longer exists and this Remove is a harmless no-op.
	defer os.Remove(tmpPath)

	if _, err := tmp.Write(binData); err != nil {
		tmp.Close()
		return "", diag.Wrap(err, diag.CodePluginTrustIO, "cannot write "+tmpPath, "check available disk space and permissions")
	}
	if err := tmp.Chmod(0o755); err != nil {
		tmp.Close()
		return "", diag.Wrap(err, diag.CodePluginTrustIO, "cannot make "+tmpPath+" executable", "check permissions on your plugin install directory")
	}
	if err := tmp.Close(); err != nil {
		return "", diag.Wrap(err, diag.CodePluginTrustIO, "cannot finalize "+tmpPath, "check available disk space and permissions")
	}
	if err := os.Rename(tmpPath, finalPath); err != nil {
		return "", diag.Wrap(err, diag.CodePluginTrustIO, "cannot install to "+finalPath, "check permissions on your plugin install directory")
	}
	return finalPath, nil
}
