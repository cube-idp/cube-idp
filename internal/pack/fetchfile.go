package pack

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cube-idp/cube-idp/internal/diag"
)

// FetchFile resolves ref — the same grammar Fetch accepts (local path,
// oci://host/repo:tag, <host>/<org>/<repo>[//subdir]@rev, git::/s3::/http(s)
// getter forms) — to the bytes of exactly ONE YAML file. It is the fetch
// primitive for spec.cluster.providerConfigRef (compose.Resolve): unlike
// Fetch it never parses pack.cue, and a ref that yields a directory must
// contain exactly one top-level *.yaml/*.yml or the fetch fails (a base
// cluster config is one document, not a tree).
func FetchFile(ctx context.Context, ref, cacheDir string) ([]byte, error) {
	switch {
	case strings.HasPrefix(ref, "oci://"):
		dir, _, err := pullOCI(ctx, strings.TrimPrefix(ref, "oci://"), cacheDir)
		if err != nil {
			return nil, err
		}
		return singleYAML(ref, dir)
	case isGitRef(ref):
		dir, _, err := fetchGitTree(ctx, ref, cacheDir)
		if err != nil {
			return nil, err
		}
		return singleYAML(ref, dir)
	case isGetterRef(ref):
		dst := filepath.Join(cacheDir, "getter", sanitizeRef(ref))
		if err := fetchGetter(ctx, ref, dst); err != nil {
			return nil, err
		}
		return singleYAML(ref, dst)
	case strings.Contains(ref, "://"):
		return nil, diag.New(diag.CodePackRefInvalid, fmt.Sprintf("unsupported ref scheme in %q", ref),
			"use a local path, oci://host/repo:tag, github.com/org/repo//path@rev, or an explicit go-getter URL (git::…, s3::…, https://…)")
	default:
		abs, err := filepath.Abs(ref)
		if err != nil {
			return nil, diag.Wrap(err, diag.CodePackRefInvalid, "bad ref path", "use a valid file or directory path")
		}
		info, err := os.Stat(abs)
		if err != nil {
			return nil, diag.Wrap(err, diag.CodePackFetchFail, fmt.Sprintf("cannot read %s", ref),
				"point the ref at a readable YAML file or a directory containing exactly one")
		}
		if info.IsDir() {
			return singleYAML(ref, abs)
		}
		b, err := os.ReadFile(abs)
		if err != nil {
			return nil, diag.Wrap(err, diag.CodePackFetchFail, fmt.Sprintf("cannot read %s", ref),
				"check file permissions")
		}
		return b, nil
	}
}

// singleYAML enforces FetchFile's shape rule on a fetched directory:
// exactly one top-level *.yaml/*.yml.
func singleYAML(ref, dir string) ([]byte, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, diag.Wrap(err, diag.CodePackFetchFail, fmt.Sprintf("cannot list fetched ref %q", ref),
			"check permissions under the cache dir")
	}
	var yamls []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if ext := filepath.Ext(e.Name()); ext == ".yaml" || ext == ".yml" {
			yamls = append(yamls, e.Name())
		}
	}
	if len(yamls) != 1 {
		return nil, diag.New(diag.CodePackFetchFail,
			fmt.Sprintf("ref %q must contain exactly one top-level YAML file, found %d", ref, len(yamls)),
			"keep a single *.yaml in the ref target, or point the ref (//subdir syntax) at the file's directory")
	}
	b, err := os.ReadFile(filepath.Join(dir, yamls[0]))
	if err != nil {
		return nil, diag.Wrap(err, diag.CodePackFetchFail, fmt.Sprintf("cannot read %s from ref %q", yamls[0], ref),
			"check permissions under the cache dir")
	}
	return b, nil
}
