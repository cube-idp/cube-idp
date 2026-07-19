package pack

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cube-idp/cube-idp/internal/diag"
)

// FetchFile resolves ref — the same grammar Fetch accepts (local path,
// oci://host/repo:tag, <host>/<org>/<repo>[//subdir]@rev, git::/s3::/http(s)
// getter forms) — to the bytes of exactly ONE YAML file plus the cube.lock
// pin of what was fetched (oci:<digest> / git+<sha> / dir:<dirhash>;
// file:<sha256-hex> for a direct local file). It is the fetch primitive for
// spec.cluster.providerConfigRef, packs[].valuesRef, engine.tuningRef and
// remote -f: unlike Fetch it never parses pack.cue, and a ref that yields a
// directory must contain exactly one top-level *.yaml/*.yml or the fetch
// fails (a config/values document is one file, not a tree).
func FetchFile(ctx context.Context, ref, cacheDir string) ([]byte, string, error) {
	switch {
	case strings.HasPrefix(ref, "oci://"):
		dir, digest, err := pullOCI(ctx, strings.TrimPrefix(ref, "oci://"), cacheDir)
		if err != nil {
			return nil, "", err
		}
		b, err := singleYAML(ref, dir)
		return b, "oci:" + digest, err
	case isGitRef(ref):
		dir, pin, err := fetchGitTree(ctx, ref, cacheDir)
		if err != nil {
			return nil, "", err
		}
		b, err := singleYAML(ref, dir)
		return b, pin, err
	case isGetterRef(ref):
		dst := filepath.Join(cacheDir, "getter", sanitizeRef(ref))
		if err := fetchGetter(ctx, ref, dst); err != nil {
			return nil, "", err
		}
		pin, err := dirPin(dst)
		if err != nil {
			return nil, "", err
		}
		b, err := singleYAML(ref, dst)
		return b, pin, err
	case strings.Contains(ref, "://"):
		return nil, "", diag.New(diag.CodePackRefInvalid, fmt.Sprintf("unsupported ref scheme in %q", ref),
			"use a local path, oci://host/repo:tag, github.com/org/repo//path@rev, or an explicit go-getter URL (git::…, s3::…, https://…)")
	default:
		abs, err := filepath.Abs(ref)
		if err != nil {
			return nil, "", diag.Wrap(err, diag.CodePackRefInvalid, "bad ref path", "use a valid file or directory path")
		}
		info, err := os.Stat(abs)
		if err != nil {
			return nil, "", diag.Wrap(err, diag.CodePackFetchFail, fmt.Sprintf("cannot read %s", ref),
				"point the ref at a readable YAML file or a directory containing exactly one")
		}
		if info.IsDir() {
			pin, err := dirPin(abs)
			if err != nil {
				return nil, "", err
			}
			b, err := singleYAML(ref, abs)
			return b, pin, err
		}
		b, err := os.ReadFile(abs)
		if err != nil {
			return nil, "", diag.Wrap(err, diag.CodePackFetchFail, fmt.Sprintf("cannot read %s", ref),
				"check file permissions")
		}
		sum := sha256.Sum256(b)
		return b, "file:" + hex.EncodeToString(sum[:]), nil
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
