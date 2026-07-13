package pack

import (
	"context"
	"fmt"

	"github.com/go-git/go-git/v5"
	gitconfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/storage/memory"

	"github.com/rafpe/cube-idp/internal/diag"
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
