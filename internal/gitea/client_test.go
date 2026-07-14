package gitea

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rafpe/cube-idp/internal/diag"
)

func giteaFake(t *testing.T, existing bool) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, _ := r.BasicAuth()
		if u != "gitea_admin" || p != "pw" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/user/repos":
			if existing {
				w.WriteHeader(http.StatusConflict)
				return
			}
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]any{
				"name": "app", "default_branch": "main",
				"clone_url": "http://gitea/gitea_admin/app.git",
				"owner":     map[string]any{"login": "gitea_admin"},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/gitea_admin/app":
			json.NewEncoder(w).Encode(map[string]any{
				"name": "app", "default_branch": "main",
				"clone_url": "http://gitea/gitea_admin/app.git",
				"owner":     map[string]any{"login": "gitea_admin"},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func TestEnsureRepoCreates(t *testing.T) {
	srv := giteaFake(t, false)
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, Username: "gitea_admin", Password: "pw"}
	repo, err := c.EnsureRepo(context.Background(), "app")
	if err != nil {
		t.Fatal(err)
	}
	if repo.Owner != "gitea_admin" || repo.DefaultBranch != "main" {
		t.Fatalf("repo: %+v", repo)
	}
}

func TestEnsureRepoIdempotentOnConflict(t *testing.T) {
	srv := giteaFake(t, true)
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, Username: "gitea_admin", Password: "pw"}
	if _, err := c.EnsureRepo(context.Background(), "app"); err != nil {
		t.Fatalf("409 must resolve to the existing repo: %v", err)
	}
}

func TestEnsureRepoBadCredentials(t *testing.T) {
	srv := giteaFake(t, false)
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, Username: "gitea_admin", Password: "wrong"}
	_, err := c.EnsureRepo(context.Background(), "app")
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != "CUBE-7302" {
		t.Fatalf("want CUBE-7302, got %v", err)
	}
}
