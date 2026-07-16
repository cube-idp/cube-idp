// Package gitea is a minimal REST client for the pack's Gitea instance —
// create-repo only, not a general git or Gitea API client. It exists so
// `cube-idp repo create` can provision a repo for the engine to deliver
// from without shelling out to git or the gitea CLI.
package gitea

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
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
