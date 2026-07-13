package pack

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	getter "github.com/hashicorp/go-getter" // RafPe fork via replace (go.mod)

	"github.com/rafpe/cube-idp/internal/diag"
)

// isGitRef: no scheme, and the first path segment looks like a hostname
// (contains a dot) — distinguishes github.com/org/repo from ./dir and packs/x.
func isGitRef(ref string) bool {
	if strings.Contains(ref, "://") || strings.Contains(ref, "::") ||
		strings.HasPrefix(ref, "/") || strings.HasPrefix(ref, ".") {
		return false
	}
	first, rest, ok := strings.Cut(ref, "/")
	return ok && rest != "" && strings.Contains(first, ".")
}

// isGetterRef: explicitly-schemed go-getter forms. oci:// is EXCLUDED — it
// stays on the oras path (digest for cube.lock + plain-HTTP for the zot
// tunnel; the fork's OCIGetter exposes neither — see the task header).
func isGetterRef(ref string) bool {
	if strings.HasPrefix(ref, "oci://") {
		return false
	}
	return strings.Contains(ref, "::") ||
		strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://")
}

// sanitizeRef turns a ref into a filesystem-safe cache-dir segment. Separate
// from pullOCI's sanitizeRepoDigest, which keys the OCI pull cache.
func sanitizeRef(ref string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case '/', ':', '?', '&', '=':
			return '_'
		}
		return r
	}, ref)
}

// fetchGetter fetches a pack via go-getter into cacheDir and applies the
// extraction guards. src is a ready go-getter URL (explicit form, or the
// translation fetchGit produced). subdir selection uses go-getter's native
// // syntax inside src.
//
// RECONCILE (verified against github.com/rafpe/go-getter v1.9.0 client.go):
// the fork's v1 Client field set matches the brief exactly — Ctx, Src, Dst,
// Mode, Detectors, Getters — plus a DisableSymlinks bool this fork adds. It
// is set here as defense in depth; GuardTree still runs unconditionally
// since DisableSymlinks is a fork-specific belt, not a cube-idp guarantee.
func fetchGetter(ctx context.Context, src, dst string) error {
	client := &getter.Client{
		Ctx:             ctx,
		Src:             src,
		Dst:             dst,
		Mode:            getter.ClientModeDir,
		DisableSymlinks: true,
		Detectors:       []getter.Detector{}, // deterministic: schemes are explicit
		Getters: map[string]getter.Getter{
			"git":   new(getter.GitGetter), // shells out to the git CLI
			"http":  new(getter.HttpGetter),
			"https": new(getter.HttpGetter),
			"s3":    new(getter.S3Getter),
			"file":  new(getter.FileGetter),
		},
	}
	if err := client.Get(); err != nil {
		return diag.Wrap(err, diag.CodePackFetchFail, fmt.Sprintf("cannot fetch pack source %q", src),
			"check the ref, your network, and that the git CLI is installed for git sources")
	}
	if _, err := GuardTree(dst); err != nil {
		_ = os.RemoveAll(dst)
		return err
	}
	return nil
}

// fetchGit resolves the bare git grammar <host>/<org>/<repo>[//subdir]@rev:
// pin first (ls-remote — fails fast, no clone on bad revs), then go-getter
// fetch of the subdir at that exact SHA.
func fetchGit(ctx context.Context, ref, cacheDir string) (*Pack, error) {
	base, rev, ok := strings.Cut(ref, "@")
	if !ok || rev == "" {
		return nil, diag.New(diag.CodePackRefUnpin,
			fmt.Sprintf("git pack ref %q is not pinned", ref),
			"append @<tag|branch|commit>, e.g. github.com/org/repo//packs/foo@v1.2.0")
	}
	repoPath, subdir, _ := strings.Cut(base, "//")
	repoURL := gitCloneURL(repoPath)

	pin, err := resolveGitPin(ctx, repoURL, rev)
	if err != nil {
		return nil, err
	}
	sha := strings.TrimPrefix(pin, "git+")

	dst := filepath.Join(cacheDir, "git", strings.ReplaceAll(repoPath, "/", "_")+"@"+sha)
	if _, statErr := os.Stat(dst); statErr != nil {
		src := fmt.Sprintf("git::%s?ref=%s", repoURL, sha)
		if subdir != "" {
			src = fmt.Sprintf("git::%s//%s?ref=%s", repoURL, subdir, sha)
		}
		if err := fetchGetter(ctx, src, dst); err != nil {
			_ = os.RemoveAll(dst)
			return nil, err
		}
	}
	p, err := loadMeta(dst)
	if err != nil {
		return nil, err
	}
	p.Pinned = pin
	return p, nil
}
