package gitea

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/cube-idp/cube-idp/internal/diag"
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

// syncFake is a Gitea fake for the P7 SyncDir/Ping surface: it serves
// /api/v1/version, a contents listing for one directory (existing:
// path→content, from which it derives git blob shas), and records every
// batch change-files POST body in *ops.
func syncFake(t *testing.T, existing map[string]string, ops *[]map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/version":
			json.NewEncoder(w).Encode(map[string]any{"version": "1.24.0"})
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/repos/gitea_admin/app/contents/manifests"):
			if len(existing) == 0 {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			var list []map[string]any
			for p, content := range existing {
				list = append(list, map[string]any{"path": p, "sha": blobSHA([]byte(content)), "type": "file"})
			}
			json.NewEncoder(w).Encode(list)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/repos/gitea_admin/app/contents":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			*ops = append(*ops, body)
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]any{"commit": map[string]any{"sha": "abc"}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func TestPing(t *testing.T) {
	var ops []map[string]any
	srv := syncFake(t, nil, &ops)
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, Username: "gitea_admin", Password: "pw"}
	if err := c.Ping(context.Background()); err != nil {
		t.Fatalf("ping against a live fake: %v", err)
	}
	srv.Close()
	if err := c.Ping(context.Background()); err == nil {
		t.Fatal("ping against a closed server must fail")
	}
}

func TestSyncDirFirstPushCreatesAll(t *testing.T) {
	var ops []map[string]any
	srv := syncFake(t, nil, &ops) // contents listing 404s: fresh repo
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, Username: "gitea_admin", Password: "pw"}
	files := map[string][]byte{
		"manifests/00-namespace-demo.yaml": []byte("kind: Namespace\n"),
		"manifests/01-configmap-cm.yaml":   []byte("kind: ConfigMap\n"),
	}
	changed, err := c.SyncDir(context.Background(), "gitea_admin", "app", "main", "manifests", "msg", files)
	if err != nil {
		t.Fatal(err)
	}
	if !changed || len(ops) != 1 {
		t.Fatalf("first push must commit once: changed=%v ops=%d", changed, len(ops))
	}
	fs := ops[0]["files"].([]any)
	if len(fs) != 2 {
		t.Fatalf("want 2 create ops, got %v", fs)
	}
	for _, f := range fs {
		op := f.(map[string]any)
		if op["operation"] != "create" {
			t.Fatalf("fresh repo push must be all creates: %v", op)
		}
		content, _ := base64.StdEncoding.DecodeString(op["content"].(string))
		if want := files[op["path"].(string)]; string(content) != string(want) {
			t.Fatalf("content mismatch for %v", op["path"])
		}
	}
	if ops[0]["branch"] != "main" || ops[0]["message"] != "msg" {
		t.Fatalf("commit metadata: %v", ops[0])
	}
}

func TestSyncDirIdempotentSkipsCommit(t *testing.T) {
	content := "kind: Namespace\n"
	var ops []map[string]any
	srv := syncFake(t, map[string]string{"manifests/00-namespace-demo.yaml": content}, &ops)
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, Username: "gitea_admin", Password: "pw"}
	changed, err := c.SyncDir(context.Background(), "gitea_admin", "app", "main", "manifests", "msg",
		map[string][]byte{"manifests/00-namespace-demo.yaml": []byte(content)})
	if err != nil {
		t.Fatal(err)
	}
	if changed || len(ops) != 0 {
		t.Fatalf("identical content must be a no-op: changed=%v ops=%d", changed, len(ops))
	}
}

func TestSyncDirUpdatesAndDeletes(t *testing.T) {
	var ops []map[string]any
	srv := syncFake(t, map[string]string{
		"manifests/00-namespace-demo.yaml": "old\n",   // differs -> update
		"manifests/09-stale-gone.yaml":     "stale\n", // absent from desired -> delete
	}, &ops)
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, Username: "gitea_admin", Password: "pw"}
	changed, err := c.SyncDir(context.Background(), "gitea_admin", "app", "main", "manifests", "msg", map[string][]byte{
		"manifests/00-namespace-demo.yaml": []byte("new\n"), // update
		"manifests/01-configmap-cm.yaml":   []byte("add\n"), // create
	})
	if err != nil {
		t.Fatal(err)
	}
	if !changed || len(ops) != 1 {
		t.Fatalf("changed content must commit once: changed=%v ops=%d", changed, len(ops))
	}
	got := map[string]string{}
	for _, f := range ops[0]["files"].([]any) {
		op := f.(map[string]any)
		got[op["path"].(string)] = op["operation"].(string)
		if op["operation"] != "create" && op["sha"] == "" {
			t.Fatalf("update/delete ops need the existing blob sha: %v", op)
		}
	}
	want := map[string]string{
		"manifests/00-namespace-demo.yaml": "update",
		"manifests/01-configmap-cm.yaml":   "create",
		"manifests/09-stale-gone.yaml":     "delete",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ops mismatch:\n got %v\nwant %v", got, want)
	}
}
