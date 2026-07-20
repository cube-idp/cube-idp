package pack

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	getter "github.com/hashicorp/go-getter" // cube-idp fork via replace (go.mod)

	"github.com/cube-idp/cube-idp/internal/diag"
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

// NeedsGitCLI reports whether ref would be fetched with the git CLI (go-getter's
// GitGetter shells out to it): the bare git grammar <host>/<org>/<repo>@<rev>
// (isGitRef), or an explicit git:: getter form. `cube-idp doctor` uses this to
// warn when git-sourced packs are configured but git isn't on PATH.
func NeedsGitCLI(ref string) bool {
	return isGitRef(ref) || strings.HasPrefix(ref, "git::")
}

// refByteSafe is the set of bytes sanitizeRef passes through unescaped:
// letters, digits, '.', '-'. Everything else — including '_' and '%'
// themselves — is percent-encoded, so the mapping is injective: distinct
// refs can never collide on the same cache-dir segment (unlike the old
// single-char-to-'_' scheme, where e.g. "a/b" and "a_b" both sanitized to
// "a_b").
func refByteSafe(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '.' || c == '-'
}

// sanitizeRef turns a ref into a filesystem-safe cache-dir segment via
// %XX percent-encoding of every unsafe byte. Separate from pullOCI's
// sanitizeRepoDigest, which keys the OCI pull cache.
func sanitizeRef(ref string) string {
	var b strings.Builder
	for i := 0; i < len(ref); i++ {
		c := ref[i]
		if refByteSafe(c) {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

// gitCacheKey builds fetchGitTree's cache-dir key from the fetched repo
// path, its resolved commit sha, and (if the ref carried one) a subdir.
// repoPath and subdir are sanitized independently and joined with '~' — a
// byte sanitizeRef never emits (it is neither refByteSafe nor a percent-hex
// digit), so it can't appear inside either encoded segment and is safe as a
// structural separator. This keeps refs into the same repo@sha but
// different subdirs (common in cnoe imports) from colliding, and keeps
// "org/a" + subdir "b/c" distinct from "org/a" + subdir "b_c" — the
// collision the old "_"-joined, "_"-escaped key had.
func gitCacheKey(repoPath, sha, subdir string) string {
	key := sanitizeRef(repoPath) + "@" + sha
	if subdir != "" {
		key += "~" + sanitizeRef(subdir)
	}
	return key
}

// fetchGetter fetches a pack via go-getter and applies the extraction
// guards. src is a ready go-getter URL (explicit form, or the translation
// fetchGit produced). subdir selection uses go-getter's native // syntax
// inside src.
//
// The fetch is atomic with respect to dst: go-getter writes into a
// temporary sibling directory, GuardTree runs on that temp tree, and only
// then is the tree renamed onto dst (os.Rename is atomic on the same
// filesystem). A crash at any point leaves at worst a .tmp-* orphan —
// never an unguarded tree at dst that a later run's cache-hit stat would
// trust. Stray .tmp-* orphans from prior crashes are swept best-effort
// before fetching.
//
// RECONCILE (verified against github.com/cube-idp/go-getter v1.9.0 client.go):
// the fork's v1 Client field set matches the brief exactly — Ctx, Src, Dst,
// Mode, Detectors, Getters — plus a DisableSymlinks bool this fork adds. It
// is set here as defense in depth; GuardTree still runs unconditionally
// since DisableSymlinks is a fork-specific belt, not a cube-idp guarantee.
func fetchGetter(ctx context.Context, src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return diag.Wrap(err, diag.CodePackFetchFail, "cannot create pack cache dir", "check permissions under the cache dir")
	}
	if stale, _ := filepath.Glob(dst + ".tmp-*"); len(stale) > 0 {
		for _, s := range stale {
			_ = os.RemoveAll(s) // best-effort sweep of prior-crash leftovers
		}
	}
	tmp := fmt.Sprintf("%s.tmp-%d", dst, os.Getpid())
	defer os.RemoveAll(tmp)

	client := &getter.Client{
		Ctx:             ctx,
		Src:             src,
		Dst:             tmp,
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
	if _, err := GuardTree(tmp); err != nil {
		return err
	}
	if err := os.RemoveAll(dst); err != nil { // the getter path re-fetches into a fixed dir
		return diag.Wrap(err, diag.CodePackFetchFail, "cannot replace previous pack cache entry", "check permissions under the cache dir")
	}
	if err := os.Rename(tmp, dst); err != nil {
		return diag.Wrap(err, diag.CodePackFetchFail, "cannot move fetched pack into the cache", "check permissions under the cache dir")
	}
	return nil
}

// FetchTree resolves the bare git grammar <host>/<org>/<repo>[//subdir]@rev
// to a local, guarded directory WITHOUT requiring pack.cue: the cnoe-compat
// loader imports plain Kubernetes manifest trees from git that
// were never authored as cube packs. Fetch/fetchGit layer pack.cue loading
// on top of the same single fetch implementation (fetchGitTree).
func FetchTree(ctx context.Context, ref, cacheDir string) (string, error) {
	dir, _, err := fetchGitTree(ctx, ref, cacheDir)
	return dir, err
}

// fetchGit resolves a bare git ref to a *Pack: the shared tree fetch, then
// pack.cue metadata plus the cube.lock pin.
func fetchGit(ctx context.Context, ref, cacheDir string) (*Pack, error) {
	dst, pin, err := fetchGitTree(ctx, ref, cacheDir)
	if err != nil {
		return nil, err
	}
	p, err := loadMeta(dst)
	if err != nil {
		return nil, err
	}
	p.Pinned = pin
	return p, nil
}

// fetchGitTree is the single git fetch implementation behind FetchTree and
// fetchGit — bare grammar <host>/<org>/<repo>[//subdir]@rev: pin first
// (ls-remote — fails fast, no clone on bad revs), then go-getter fetch of
// the subdir at that exact SHA. Returns the on-disk tree and its
// "git+<sha>" pin.
func fetchGitTree(ctx context.Context, ref, cacheDir string) (dir, pin string, err error) {
	base, rev, ok := strings.Cut(ref, "@")
	if !ok || rev == "" {
		return "", "", diag.New(diag.CodePackRefUnpin,
			fmt.Sprintf("git pack ref %q is not pinned", ref),
			"append @<tag|branch|commit>, e.g. github.com/org/repo//packs/foo@v1.2.0")
	}
	repoPath, subdir, _ := strings.Cut(base, "//")
	repoURL := gitCloneURL(repoPath)

	pin, err = resolveGitPin(ctx, repoURL, rev)
	if err != nil {
		return "", "", err
	}
	sha := strings.TrimPrefix(pin, "git+")

	// The subdir is part of the cache key: only the subdir's tree is fetched
	// into dst, so two refs into the same repo@sha but different subdirs
	// (common in cnoe imports — many Applications, one repo) must not share
	// a cache entry.
	dst := filepath.Join(cacheDir, "git", gitCacheKey(repoPath, sha, subdir))
	if _, statErr := os.Stat(dst); statErr != nil {
		src := fmt.Sprintf("git::%s?ref=%s", repoURL, sha)
		if subdir != "" {
			src = fmt.Sprintf("git::%s//%s?ref=%s", repoURL, subdir, sha)
		}
		// fetchGetter is atomic: on any failure nothing exists at dst,
		// so the cache-hit stat above can never trust a partial tree.
		if err := fetchGetter(ctx, src, dst); err != nil {
			return "", "", err
		}
	}
	return dst, pin, nil
}
