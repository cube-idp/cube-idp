package argocd

import (
	"context"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/cube-idp/cube-idp/internal/engine"
)

// DeliverGit is the git-sourced counterpart of Deliver: a single Argo CD
// Application named cube-idp-<name> whose source is a git repository (URL +
// branch + path) instead of the zot OCI registry. It reuses the exact same
// Application scaffolding as Deliver, changing only spec.source. Same purity
// rule — it RETURNS the object, the caller applies it. Empty Branch defaults
// to "main" and empty Path to "./".
func (g *ArgoCD) DeliverGit(ctx context.Context, name string, src engine.GitSource) ([]*unstructured.Unstructured, error) {
	branch := src.Branch
	if branch == "" {
		branch = "main"
	}
	path := src.Path
	if path == "" {
		path = "./"
	}
	return []*unstructured.Unstructured{application(deliveryName(name), map[string]any{
		"repoURL":        src.URL,
		"targetRevision": branch,
		"path":           path,
	})}, nil
}
