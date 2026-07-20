package pack

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/go-git/go-git/v5"
	gitconfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/storage/memory"
	"oras.land/oras-go/v2/registry/remote"

	"github.com/cube-idp/cube-idp/internal/diag"
)

// gitCloneURL maps the repo path from a pack ref to a cloneable URL.
// Overridden in tests to point at a local fixture repository.
var gitCloneURL = func(repoPath string) string { return "https://" + repoPath }

// resolveGitPin returns "git+<full-sha>" for rev in the repo — the pin
// recorded in Pack.Pinned and cube.lock. go-git's in-memory ls-remote; no
// clone, no git CLI. A 40-char rev is its own pin.
func resolveGitPin(ctx context.Context, repoURL, rev string) (string, error) {
	if len(rev) == 40 {
		return "git+" + rev, nil
	}
	rem := git.NewRemote(memory.NewStorage(), &gitconfig.RemoteConfig{
		Name: "origin", URLs: []string{repoURL},
	})
	refs, err := rem.ListContext(ctx, &git.ListOptions{})
	if err != nil {
		return "", diag.Wrap(err, diag.CodePackFetchFail, fmt.Sprintf("cannot list refs of %s", repoURL),
			"check the repo path and your network")
	}
	for _, r := range refs {
		n := r.Name()
		if n.Short() == rev || n.String() == "refs/tags/"+rev || n.String() == "refs/heads/"+rev {
			return "git+" + r.Hash().String(), nil
		}
	}
	return "", diag.New(diag.CodePackFetchFail, fmt.Sprintf("revision %q not found in %s", rev, repoURL),
		"use a tag, branch, or full commit SHA that exists in the repository")
}

// ResolveRemote returns the current upstream pin for ref without pulling
// content (http/s3 getter refs excepted — no pin protocol, so they are
// probed by fetch+dirhash). It is upgrade --plan's probe: cube.lock's
// Resolved field records what we HAVE; this returns what we WOULD get.
func ResolveRemote(ctx context.Context, ref, cacheDir string) (string, error) {
	switch {
	case strings.HasPrefix(ref, "oci://"):
		name := strings.TrimPrefix(ref, "oci://")
		repo, err := remote.NewRepository(name)
		if err != nil {
			return "", diag.Wrap(err, diag.CodeDigestResolveFail, fmt.Sprintf("bad OCI ref %q", ref), "use oci://host/repo:tag")
		}
		// Mirrors pullOCI's client setup exactly (docker credential chain via
		// RegistryClient; plain HTTP only for the 127.0.0.1/localhost zot
		// tunnel) so both paths trust and authenticate to the same hosts
		// identically — a real registry (e.g. ghcr.io) still uses HTTPS here,
		// just as it does on the fetch path.
		client, err := RegistryClient()
		if err != nil {
			return "", diag.Wrap(err, diag.CodeDigestResolveFail, "cannot load docker credential store",
				"check ~/.docker/config.json (run `docker login <registry>` to create it)")
		}
		repo.Client = client
		if IsLocalRegistryHost(repo.Reference.Registry) {
			repo.PlainHTTP = true
		}
		tagOrDigest := repo.Reference.Reference
		if tagOrDigest == "" {
			return "", diag.New(diag.CodeDigestResolveFail, fmt.Sprintf("OCI pack ref %q has no tag or digest", ref),
				"use the form oci://host/repo:tag")
		}
		desc, err := repo.Resolve(ctx, tagOrDigest)
		if err != nil {
			return "", diag.Wrap(err, diag.CodeDigestResolveFail,
				fmt.Sprintf("cannot resolve %s from the registry", ref),
				"check network access to the registry and that the tag exists")
		}
		return "oci:" + desc.Digest.String(), nil

	case isGitRef(ref):
		base, rev, ok := strings.Cut(ref, "@")
		if !ok || rev == "" {
			return "", diag.New(diag.CodePackRefUnpin, fmt.Sprintf("git pack ref %q is not pinned", ref),
				"append @<tag|branch|commit>")
		}
		repoPath, _, _ := strings.Cut(base, "//")
		return resolveGitPin(ctx, gitCloneURL(repoPath), rev) // shared with Fetch — one ls-remote implementation

	case isGetterRef(ref):
		// http/s3 sources have no cheap upstream pin: fetch to a probe dir
		// and dirhash it — identical semantics to what Fetch records.
		dst := filepath.Join(cacheDir, "probe", sanitizeRef(ref))
		if err := fetchGetter(ctx, ref, dst); err != nil {
			return "", err
		}
		return dirPin(dst)

	default:
		abs, err := filepath.Abs(ref)
		if err != nil {
			return "", diag.Wrap(err, diag.CodePackRefInvalid, "bad pack path", "use a valid directory path")
		}
		return dirPin(abs) // the same dirhash helper Fetch records pins with
	}
}
