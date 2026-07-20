// Package gitea is a minimal REST client for the pack's Gitea instance —
// create-repo only, not a general git or Gitea API client. It exists so
// `cube-idp repo create` can provision a repo for the engine to deliver
// from without shelling out to git or the gitea CLI.
package gitea

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	neturl "net/url"
	"sort"
	"time"

	"github.com/cube-idp/cube-idp/internal/diag"
)

// requestTimeout bounds every Gitea API call so a wedged port-forward or
// unresponsive pod can never hang `repo create` indefinitely (deadline
// rule).
const requestTimeout = 10 * time.Second

// Client talks to a single Gitea instance's REST API. BaseURL is expected
// to be the local end of a port-forward tunnel (e.g. "http://127.0.0.1:PORT"
// from kube.PortForward) — Client itself has no notion of the cluster.
type Client struct {
	BaseURL  string
	Username string
	Password string
}

// Repo is the subset of Gitea's repository representation callers need.
type Repo struct {
	Owner         string
	Name          string
	CloneURL      string
	DefaultBranch string
}

// repoResponse mirrors the fields of Gitea's repository JSON this package
// actually consumes.
type repoResponse struct {
	Name          string `json:"name"`
	DefaultBranch string `json:"default_branch"`
	CloneURL      string `json:"clone_url"`
	Owner         struct {
		Login string `json:"login"`
	} `json:"owner"`
}

func (r repoResponse) toRepo() *Repo {
	return &Repo{Owner: r.Owner.Login, Name: r.Name, CloneURL: r.CloneURL, DefaultBranch: r.DefaultBranch}
}

// EnsureRepo creates a repository named name for the admin user, with
// auto_init (so the default branch exists for the engine to sync) and
// private=false (no pull secret needed in-cluster; local-dev posture, same
// as the pack's fixed admin password — documented in the command help).
// Idempotent: a 409 (repo already exists) resolves to a GET of the existing
// repo instead of failing. Any other failure (including bad credentials,
// 401) is CUBE-7302.
func (c *Client) EnsureRepo(ctx context.Context, name string) (*Repo, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	body, err := json.Marshal(map[string]any{
		"name":           name,
		"auto_init":      true,
		"private":        false,
		"default_branch": "main",
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/v1/user/repos", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(c.Username, c.Password)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusCreated:
		var rr repoResponse
		if err := json.NewDecoder(resp.Body).Decode(&rr); err != nil {
			return nil, err
		}
		return rr.toRepo(), nil
	case http.StatusConflict:
		return c.getRepo(ctx, c.Username, name)
	default:
		return nil, giteaAPIError(resp, req)
	}
}

// getRepo fetches an existing repository by owner/name, used by EnsureRepo
// to resolve the 409 case idempotently.
func (c *Client) getRepo(ctx context.Context, owner, name string) (*Repo, error) {
	url := fmt.Sprintf("%s/api/v1/repos/%s/%s", c.BaseURL, owner, name)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(c.Username, c.Password)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, giteaAPIError(resp, req)
	}
	var rr repoResponse
	if err := json.NewDecoder(resp.Body).Decode(&rr); err != nil {
		return nil, err
	}
	return rr.toRepo(), nil
}

// giteaAPIError wraps an unexpected Gitea API response (including 401 bad
// credentials) as CUBE-7302.
func giteaAPIError(resp *http.Response, req *http.Request) error {
	return diag.New(diag.CodeRepoGiteaAPIFail,
		fmt.Sprintf("Gitea API returned %s for %s", resp.Status, req.URL.Path),
		"check the gitea pod (`kubectl -n gitea get pods`) and credentials (`cube-idp get secrets -p gitea`)")
}

// commitTimeout bounds SyncDir's batch change-files POST: one commit that
// may carry every rendered manifest of a pack, so it gets a wider budget
// than the 10s per-call requestTimeout.
const commitTimeout = 30 * time.Second

// Ping probes the Gitea API (GET /api/v1/version) — the cheap "is gitea
// answering yet" check behind the repo-delivery readiness gate (engine
// delivery is asynchronous: the gitea pack being delivered does not mean
// its API serves). The error is deliberately untyped: callers poll Ping
// and type only the terminal timeout (CUBE-7301).
func (c *Client) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/api/v1/version", nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth(c.Username, c.Password)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("gitea API returned %s for %s", resp.Status, req.URL.Path)
	}
	return nil
}

// contentsEntry is the subset of Gitea's contents-list JSON SyncDir reads.
type contentsEntry struct {
	Path string `json:"path"`
	SHA  string `json:"sha"`
	Type string `json:"type"`
}

// changeFileOp is one entry of Gitea's batch change-files payload
// (POST /api/v1/repos/{owner}/{repo}/contents, Gitea >= 1.18).
type changeFileOp struct {
	Operation string `json:"operation"` // create | update | delete
	Path      string `json:"path"`
	Content   string `json:"content,omitempty"` // base64; create/update only
	SHA       string `json:"sha,omitempty"`     // existing blob sha; update/delete only
}

// SyncDir makes the repo's dir/ subtree on branch exactly match files
// (path -> content; every path must live under dir/, flat) in at most ONE
// commit: unchanged files (matching git blob sha) are skipped, changed
// ones updated, missing ones created, and tracked files that vanished
// from the desired set deleted. Content identical to the repo means no
// commit at all — a re-run `up` leaves no commit churn. Files OUTSIDE
// dir/ (and in subdirectories of it) are never touched: the repo stays an
// editable working copy everywhere else (the payoff of keeping gitea an
// ordinary, operator-owned repo rather than a cube-idp-owned one); dir/ is
// the render's, and a re-push overwrites in-repo edits to it (cube.yaml
// is the source of truth). Reports whether a commit was made.
func (c *Client) SyncDir(ctx context.Context, owner, repo, branch, dir, message string, files map[string][]byte) (bool, error) {
	existing, err := c.listDir(ctx, owner, repo, branch, dir)
	if err != nil {
		return false, err
	}

	var ops []changeFileOp
	for path, content := range files {
		sha, tracked := existing[path]
		switch {
		case !tracked:
			ops = append(ops, changeFileOp{Operation: "create", Path: path,
				Content: base64.StdEncoding.EncodeToString(content)})
		case sha != blobSHA(content):
			ops = append(ops, changeFileOp{Operation: "update", Path: path, SHA: sha,
				Content: base64.StdEncoding.EncodeToString(content)})
		}
	}
	for path, sha := range existing {
		if _, wanted := files[path]; !wanted {
			ops = append(ops, changeFileOp{Operation: "delete", Path: path, SHA: sha})
		}
	}
	if len(ops) == 0 {
		return false, nil
	}
	// Stable op order (path-sorted): deterministic payloads for a
	// deterministic single commit.
	sort.Slice(ops, func(i, j int) bool { return ops[i].Path < ops[j].Path })

	body, err := json.Marshal(map[string]any{
		"branch":  branch,
		"message": message,
		"files":   ops,
	})
	if err != nil {
		return false, err
	}
	cctx, cancel := context.WithTimeout(ctx, commitTimeout)
	defer cancel()
	url := fmt.Sprintf("%s/api/v1/repos/%s/%s/contents", c.BaseURL, owner, repo)
	req, err := http.NewRequestWithContext(cctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return false, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(c.Username, c.Password)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return false, giteaAPIError(resp, req)
	}
	return true, nil
}

// listDir returns the tracked flat files under dir/ on branch as
// path -> blob sha. A 404 means the directory does not exist yet (fresh
// auto_init repo) — an empty map, not an error.
func (c *Client) listDir(ctx context.Context, owner, repo, branch, dir string) (map[string]string, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	url := fmt.Sprintf("%s/api/v1/repos/%s/%s/contents/%s?ref=%s", c.BaseURL, owner, repo, dir, neturl.QueryEscape(branch))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(c.Username, c.Password)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return map[string]string{}, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, giteaAPIError(resp, req)
	}
	var entries []contentsEntry
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return nil, err
	}
	out := make(map[string]string, len(entries))
	for _, e := range entries {
		if e.Type == "file" {
			out[e.Path] = e.SHA
		}
	}
	return out, nil
}

// blobSHA is content's git blob sha1 ("blob <len>\x00" + content) — the
// sha Gitea reports for a file, used to skip no-op pushes. sha1 is git's
// own content addressing here, not a security boundary.
func blobSHA(content []byte) string {
	h := sha1.New()
	fmt.Fprintf(h, "blob %d\x00", len(content))
	h.Write(content)
	return hex.EncodeToString(h.Sum(nil))
}
