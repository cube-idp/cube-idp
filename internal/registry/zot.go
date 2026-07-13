// Package registry embeds and installs the in-cluster zot OCI registry —
// the delivery bus between cube-idp's client-side rendering and the GitOps
// engine (spec §4, "OCI push").
package registry

import (
	"context"
	_ "embed"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/rafpe/cube-idp/internal/apply"
	"github.com/rafpe/cube-idp/internal/diag"
)

const InClusterURL = "zot.cube-idp-system.svc.cluster.local:5000"

//go:embed manifests/zot.yaml
var zotYAML []byte

func Manifests() ([]*unstructured.Unstructured, error) {
	return apply.ParseMultiDoc(zotYAML)
}

func Install(ctx context.Context, a *apply.Applier, timeout time.Duration) error {
	objs, err := Manifests()
	if err != nil {
		return diag.Wrap(err, diag.CodeZotManifestsInv, "embedded zot manifests are invalid", "this is a cube-idp bug — please report it")
	}
	return a.Apply(ctx, objs, true, timeout)
}
