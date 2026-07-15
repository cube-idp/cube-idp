package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/rafpe/cube-idp/internal/apply"
	"github.com/rafpe/cube-idp/internal/cluster"
	"github.com/rafpe/cube-idp/internal/config"
	"github.com/rafpe/cube-idp/internal/diag"
	"github.com/rafpe/cube-idp/internal/engine"
	enginefactory "github.com/rafpe/cube-idp/internal/engine/factory"
	"github.com/rafpe/cube-idp/internal/gitea"
	"github.com/rafpe/cube-idp/internal/kube"
	"github.com/rafpe/cube-idp/internal/pack"
	"github.com/rafpe/cube-idp/internal/ui"
)

const (
	repoClusterTimeout = 3 * time.Minute
	repoDeployTimeout  = 2 * time.Minute

	// giteaNamespace, giteaAdminSecretName, giteaPodSelector, giteaPodPort
	// and giteaInClusterHost are the D11-verified facts about the shipped
	// gitea pack (checkpoint 0.10/0.8): admin Secret gitea-admin-cube-idp in
	// namespace gitea (keys username/password); chart-standard pod label
	// app.kubernetes.io/name=gitea; Service gitea-http:3000 (chart 12.6.0).
	giteaNamespace        = "gitea"
	giteaAdminSecretName  = "gitea-admin-cube-idp"
	giteaPodSelector      = "app.kubernetes.io/name=gitea"
	giteaPodPort          = 3000
	giteaInClusterHost    = "gitea-http.gitea.svc.cluster.local:3000"
	repoDeliverGitDefault = "./"
)

func newRepoCmd() *cobra.Command {
	repo := &cobra.Command{
		Use:   "repo",
		Short: "Manage git repositories in the cube's built-in Gitea",
	}
	repo.AddCommand(newRepoCreateCmd())
	return repo
}

func newRepoCreateCmd() *cobra.Command {
	var file string
	var deploy bool
	c := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a Gitea repo, optionally wiring it up as an engine delivery source",
		Long: `Create a Gitea repo, optionally wiring it up as an engine delivery source.

The repo is created for the gitea pack's admin user with auto_init (so it
has a default branch to push to and, with --deploy, for the engine to sync
from immediately) and is not private — no pull secret is needed for the
in-cluster engine to clone it. This matches the gitea pack's own local-dev
posture (a fixed admin password suitable for a local dev platform, not for
anything internet-facing).

Re-running this command for the same name is safe: repo creation is
idempotent, and --deploy re-registers the same delivery source.`,
		Args: cobra.ExactArgs(1),
		// RunPipelineStatic owns the whole RunE body (Task R3): a failed
		// config.Load returns before con.Start ever fires (the
		// RunStarted-skip rule, G6). repo create is a short static command
		// — it never pops the live step-tree (reserved for vendor/up/down).
		RunE: func(c *cobra.Command, args []string) error {
			name := args[0]
			return ui.RunPipelineStatic(c.Context(), "repo", c.OutOrStdout(),
				func(ctx context.Context, con *ui.Console) error {
					cube, err := config.Load(file)
					if err != nil {
						return err
					}
					con.Start("repo", cube.Metadata.Name)

					prov, err := cluster.New(cube.Spec.Cluster, cube.Spec.Gateway)
					if err != nil {
						return err
					}
					// repo create targets an already-`up` cube: Ensure would
					// CREATE a missing kind cluster, which repo create must
					// never do as a side effect (status/get/sync pattern).
					if err := requireClusterExists(ctx, prov, cube.Spec.Cluster.Provider, cube.Metadata.Name); err != nil {
						return err
					}
					ensureCtx, cancel := context.WithTimeout(ctx, repoClusterTimeout)
					conn, err := prov.Ensure(ensureCtx, cube.Metadata.Name, cube.Spec.Cluster)
					cancel()
					if err != nil {
						return err
					}
					a, err := apply.New(conn.REST, cube.Metadata.Name)
					if err != nil {
						return err
					}

					var sec corev1.Secret
					key := client.ObjectKey{Namespace: giteaNamespace, Name: giteaAdminSecretName}
					if err := a.Client().Get(ctx, key, &sec); err != nil {
						return diag.Wrap(err, diag.CodeRepoGiteaUnavailable, "the gitea pack is not installed in this cube",
							"add the gitea pack to cube.yaml and re-run `cube-idp up`")
					}
					username, password := string(sec.Data["username"]), string(sec.Data["password"])

					addr, stop, err := kube.PortForward(ctx, conn.REST, giteaNamespace, giteaPodSelector, giteaPodPort)
					if err != nil {
						return diag.Wrap(err, diag.CodeRepoGiteaUnavailable, "cannot reach the gitea pod",
							"check `kubectl -n gitea get pods`; if the gitea pack isn't installed, add it to cube.yaml and re-run `cube-idp up`")
					}
					defer stop()

					gc := &gitea.Client{BaseURL: "http://" + addr, Username: username, Password: password}
					repoInfo, err := gc.EnsureRepo(ctx, name)
					if err != nil {
						return err
					}

					if deploy {
						eng, err := enginefactory.New(cube.Spec.Engine.Type)
						if err != nil {
							return err
						}
						if err := deployRepo(ctx, a, eng, name, repoInfo); err != nil {
							return err
						}
					}

					emitRepoAccess(con, cube.Spec.Gateway, repoInfo, deploy)
					return nil
				})
		},
	}
	c.Flags().StringVarP(&file, "file", "f", "cube.yaml", "path to cube.yaml")
	c.Flags().BoolVar(&deploy, "deploy", false, "register the repo as a continuously-synced engine delivery source")
	return c
}

// deployRepo registers repoInfo with the cube's engine as a continuously-
// synced git delivery source and applies the resulting objects. It uses the
// in-cluster gitea Service URL (giteaInClusterHost) — the ENGINE clones the
// repo from inside the cluster, never from the laptop's port-forward tunnel
// (recorded 0.10 decision). Any failure here is CUBE-7303: the repo itself
// already exists (EnsureRepo is idempotent), so the remediation is simply
// to re-run `repo create --deploy`.
func deployRepo(ctx context.Context, a *apply.Applier, eng engine.Engine, name string, repoInfo *gitea.Repo) error {
	remediation := fmt.Sprintf("re-run `cube-idp repo create %s --deploy` — repo creation is idempotent", name)

	src := engine.GitSource{
		URL:    fmt.Sprintf("http://%s/%s/%s.git", giteaInClusterHost, repoInfo.Owner, repoInfo.Name),
		Branch: repoInfo.DefaultBranch,
		Path:   repoDeliverGitDefault,
	}
	objs, err := eng.DeliverGit(ctx, name, src)
	if err != nil {
		return diag.Wrap(err, diag.CodeRepoDeployFail, "created the repo but could not register the deploy source", remediation)
	}
	if err := a.Apply(ctx, objs, false, repoDeployTimeout); err != nil {
		return diag.Wrap(err, diag.CodeRepoDeployFail, "created the repo but could not register the deploy source", remediation)
	}
	if err := a.RecordInventory(ctx, objs); err != nil {
		return diag.Wrap(err, diag.CodeRepoDeployFail, "created the repo but could not register the deploy source", remediation)
	}
	return nil
}

// repoCloneURL is the printed, operator-facing clone URL: the https gateway
// form (real TLS via the gateway, checkpoint 0.8), NOT the in-cluster URL
// deployRepo hands the engine — a human clones/pushes over the gateway, the
// engine reaches the Service directly.
func repoCloneURL(gw config.GatewaySpec, r *gitea.Repo) string {
	return fmt.Sprintf("https://gitea.%s/%s/%s.git", pack.GatewayHostString(gw), r.Owner, r.Name)
}

// emitRepoAccess emits the access block for a freshly ensured repo as Note
// events: the created confirmation, the clone URL, a ready-to-copy push
// command, and (only when deployed) a one-line note that the engine is
// syncing it. Each old Fprintf ended with exactly one "\n"; Note's plain
// projection adds exactly one trailing newline per call — byte-identical
// (Task R3). The "✔" is the plain-mode literal p.Glyph(ui.GlyphOK) rendered
// before this migration (Glyph is a no-op passthrough in ModePlain).
// Deliberate styled-mode delta (repo create's styled output was never
// pinned): the ✔ loses its green styling in the Styled projection — Note's
// styled form is Fprintln, same as Plain's, with no glyph coloring.
func emitRepoAccess(con *ui.Console, gw config.GatewaySpec, r *gitea.Repo, deployed bool) {
	clone := repoCloneURL(gw, r)
	con.Note("✔ repo %s/%s created", r.Owner, r.Name)
	con.Note("  clone:  %s", clone)
	con.Note("  push:   git push %s %s", clone, r.DefaultBranch)
	if deployed {
		con.Note("  deploy: engine syncs %s on branch %s (--deploy)", repoDeliverGitDefault, r.DefaultBranch)
	}
}
