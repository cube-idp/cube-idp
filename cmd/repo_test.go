package cmd

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/cube-idp/cube-idp/internal/apply"
	"github.com/cube-idp/cube-idp/internal/config"
	"github.com/cube-idp/cube-idp/internal/diag"
	"github.com/cube-idp/cube-idp/internal/engine"
	"github.com/cube-idp/cube-idp/internal/gitea"
	"github.com/cube-idp/cube-idp/internal/pack"
	"github.com/cube-idp/cube-idp/internal/ui"
)

// failingDeliverGitEngine is a minimal engine.Engine stub whose only
// meaningful method is DeliverGit, which always fails — used to prove
// deployRepo wraps a DeliverGit failure as CUBE-7303 with idempotent-re-run
// remediation, without needing a real cluster or engine implementation.
type failingDeliverGitEngine struct{}

func (failingDeliverGitEngine) Install(context.Context, *apply.Applier, time.Duration) error {
	return nil
}
func (failingDeliverGitEngine) InstallManifests() ([]*unstructured.Unstructured, error) {
	return nil, nil
}
func (failingDeliverGitEngine) Deliver(context.Context, *pack.Rendered, engine.ArtifactRef) ([]*unstructured.Unstructured, error) {
	return nil, nil
}
func (failingDeliverGitEngine) DeliverGit(context.Context, string, engine.GitSource, []string) ([]*unstructured.Unstructured, error) {
	return nil, errors.New("boom")
}
func (failingDeliverGitEngine) DeliverSelf(context.Context, engine.ArtifactRef) ([]*unstructured.Unstructured, error) {
	return nil, nil
}
func (failingDeliverGitEngine) Poke(context.Context, *apply.Applier, string) error { return nil }
func (failingDeliverGitEngine) Health(context.Context, *apply.Applier) ([]engine.ComponentHealth, error) {
	return nil, nil
}
func (failingDeliverGitEngine) Uninstall(context.Context, *apply.Applier, time.Duration) error {
	return nil
}
func (failingDeliverGitEngine) OrdersDeliveries() bool { return true }

func TestDeployRepoWrapsDeliverGitFailureAsCUBE7303(t *testing.T) {
	repoInfo := &gitea.Repo{Owner: "gitea_admin", Name: "app", DefaultBranch: "main"}
	// a is nil: DeliverGit fails before deployRepo ever touches the Applier.
	err := deployRepo(context.Background(), nil, failingDeliverGitEngine{}, "app", repoInfo)

	var de *diag.Error
	if !errors.As(err, &de) || de.Code != diag.CodeRepoDeployFail {
		t.Fatalf("want %s, got %v", diag.CodeRepoDeployFail, err)
	}
	if !strings.Contains(de.Remediation, "repo create app --deploy") || !strings.Contains(de.Remediation, "idempotent") {
		t.Fatalf("remediation must point at re-running --deploy (idempotent repo creation), got: %q", de.Remediation)
	}
}

func TestRepoCloneURLUsesHTTPSGatewayForm(t *testing.T) {
	gw := config.GatewaySpec{Host: "cube-idp.localtest.me", Port: 8443}
	r := &gitea.Repo{Owner: "gitea_admin", Name: "app"}
	got := repoCloneURL(gw, r)
	want := "https://gitea.cube-idp.localtest.me:8443/gitea_admin/app.git"
	if got != want {
		t.Fatalf("repoCloneURL: got %q, want %q", got, want)
	}
}

func TestRepoCloneURLOmitsDefaultHTTPSPort(t *testing.T) {
	gw := config.GatewaySpec{Host: "cube-idp.example.com", Port: 443}
	r := &gitea.Repo{Owner: "gitea_admin", Name: "app"}
	got := repoCloneURL(gw, r)
	want := "https://gitea.cube-idp.example.com/gitea_admin/app.git"
	if got != want {
		t.Fatalf("repoCloneURL: got %q, want %q", got, want)
	}
}

// TestEmitRepoAccessPlainByteStable is the byte-freeze golden for repo
// create's access block: emitRepoAccess driven through ui.RunPipeline with
// ModePlain forced must produce exactly the pinned four-line block — the same
// literal cmd/repo_test.go's TestNewRepoCmd end-to-end assertion (line ~91)
// pins, isolated to the emitRepoAccess seam.
func TestEmitRepoAccessPlainByteStable(t *testing.T) {
	gw := config.GatewaySpec{Host: "cube-idp.localtest.me", Port: 8443}
	r := &gitea.Repo{Owner: "gitea_admin", Name: "app", DefaultBranch: "main"}

	var buf bytes.Buffer
	err := ui.RunPipeline(context.Background(), "repo", &buf,
		func(_ context.Context, con *ui.Console) error {
			emitRepoAccess(con, gw, r, false)
			return nil
		})
	if err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	want := "✔ repo gitea_admin/app created\n" +
		"  clone:  https://gitea.cube-idp.localtest.me:8443/gitea_admin/app.git\n" +
		"  push:   git push https://gitea.cube-idp.localtest.me:8443/gitea_admin/app.git main\n"
	if got != want {
		t.Fatalf("emitRepoAccess (no deploy):\ngot:\n%q\nwant:\n%q", got, want)
	}
	if strings.Contains(got, "deploy:") {
		t.Fatalf("emitRepoAccess must not print a deploy line when deployed=false, got:\n%s", got)
	}
}

func TestEmitRepoAccessWithDeployPlainByteStable(t *testing.T) {
	gw := config.GatewaySpec{Host: "cube-idp.localtest.me", Port: 8443}
	r := &gitea.Repo{Owner: "gitea_admin", Name: "app", DefaultBranch: "main"}

	var buf bytes.Buffer
	err := ui.RunPipeline(context.Background(), "repo", &buf,
		func(_ context.Context, con *ui.Console) error {
			emitRepoAccess(con, gw, r, true)
			return nil
		})
	if err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	if !strings.Contains(got, "deploy: engine syncs ./ on branch main (--deploy)\n") {
		t.Fatalf("emitRepoAccess (deploy) missing deploy line, got:\n%s", got)
	}
}

func TestNewRepoCmdWiring(t *testing.T) {
	repo := newRepoCmd()
	if repo.Use != "repo" {
		t.Fatalf("Use = %q, want %q", repo.Use, "repo")
	}
	create, _, err := repo.Find([]string{"create"})
	if err != nil {
		t.Fatalf("repo create not registered: %v", err)
	}
	if create.Flags().Lookup("deploy") == nil {
		t.Fatal("repo create missing --deploy flag")
	}
	if create.Flags().Lookup("file") == nil {
		t.Fatal("repo create missing --file flag")
	}
}
