package cmd

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/rafpe/cube-idp/internal/apply"
	"github.com/rafpe/cube-idp/internal/config"
	"github.com/rafpe/cube-idp/internal/diag"
	"github.com/rafpe/cube-idp/internal/engine"
	"github.com/rafpe/cube-idp/internal/gitea"
	"github.com/rafpe/cube-idp/internal/pack"
	"github.com/rafpe/cube-idp/internal/ui"
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
func (failingDeliverGitEngine) DeliverGit(context.Context, string, engine.GitSource) ([]*unstructured.Unstructured, error) {
	return nil, errors.New("boom")
}
func (failingDeliverGitEngine) Poke(context.Context, *apply.Applier, string) error { return nil }
func (failingDeliverGitEngine) Health(context.Context, *apply.Applier) ([]engine.ComponentHealth, error) {
	return nil, nil
}
func (failingDeliverGitEngine) Uninstall(context.Context, *apply.Applier, time.Duration) error {
	return nil
}

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

func TestPrintRepoAccessWithoutDeploy(t *testing.T) {
	var buf bytes.Buffer
	p := ui.New(&buf, false) // not a TTY -> plain mode, deterministic bytes
	gw := config.GatewaySpec{Host: "cube-idp.localtest.me", Port: 8443}
	r := &gitea.Repo{Owner: "gitea_admin", Name: "app", DefaultBranch: "main"}

	printRepoAccess(&buf, p, gw, r, false)

	got := buf.String()
	want := "✔ repo gitea_admin/app created\n" +
		"  clone:  https://gitea.cube-idp.localtest.me:8443/gitea_admin/app.git\n" +
		"  push:   git push https://gitea.cube-idp.localtest.me:8443/gitea_admin/app.git main\n"
	if got != want {
		t.Fatalf("printRepoAccess (no deploy):\ngot:\n%q\nwant:\n%q", got, want)
	}
	if strings.Contains(got, "deploy:") {
		t.Fatalf("printRepoAccess must not print a deploy line when deployed=false, got:\n%s", got)
	}
}

func TestPrintRepoAccessWithDeploy(t *testing.T) {
	var buf bytes.Buffer
	p := ui.New(&buf, false)
	gw := config.GatewaySpec{Host: "cube-idp.localtest.me", Port: 8443}
	r := &gitea.Repo{Owner: "gitea_admin", Name: "app", DefaultBranch: "main"}

	printRepoAccess(&buf, p, gw, r, true)

	got := buf.String()
	if !strings.Contains(got, "deploy: engine syncs ./ on branch main (--deploy)\n") {
		t.Fatalf("printRepoAccess (deploy) missing deploy line, got:\n%s", got)
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
