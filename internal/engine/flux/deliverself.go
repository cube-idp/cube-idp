package flux

import (
	"context"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/cube-idp/cube-idp/internal/engine"
	"github.com/cube-idp/cube-idp/internal/registry"
)

// DeliverSelf translates the pushed cube-engine artifact (GT16, P8) into
// the flux objects through which the engine manages ITSELF: one
// OCIRepository (source) + one Kustomization (apply), both named
// cube-engine in flux-system. The ref/auth shape mirrors Deliver exactly
// (same zot URL derivation, tag ref, insecure plain-HTTP flag) — the
// artifact is pushed by the same oci.PushRendered, so the pull path with
// its media-type constraints is already solved there. Differences from
// Deliver, each load-bearing:
//
//   - prune: false — the engine must never prune its own controllers out
//     from under itself (and deleting the Kustomization on `down` must not
//     cascade; the inventory-driven DeleteAll owns engine removal).
//   - the OCIRepository carries a fresh reconcile.fluxcd.io/requestedAt
//     stamp, so each `up` apply doubles as the GT16 "poke": push → apply →
//     source refetches the tag now instead of on its interval. Poke(name)
//     cannot address this object (it prefixes cube-idp-<pack>).
//
// Same purity rule as Deliver: RETURNS objects, never touches the cluster.
func (f *Flux) DeliverSelf(ctx context.Context, src engine.ArtifactRef) ([]*unstructured.Unstructured, error) {
	name := engine.SelfArtifactName
	repo := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "source.toolkit.fluxcd.io/v1",
		"kind":       "OCIRepository",
		"metadata": map[string]any{
			"name": name, "namespace": fluxNS,
			"annotations": map[string]any{pokeAnnotation: time.Now().Format(time.RFC3339Nano)},
		},
		"spec": map[string]any{
			"interval": "1m",
			"url":      fmt.Sprintf("oci://%s/%s", registry.InClusterURL, src.Repo),
			"ref":      map[string]any{"tag": src.Tag},
			"insecure": true, // zot is plain HTTP inside the cluster
		},
	}}
	kust := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "kustomize.toolkit.fluxcd.io/v1",
		"kind":       "Kustomization",
		"metadata":   map[string]any{"name": name, "namespace": fluxNS},
		"spec": map[string]any{
			"interval": "10m",
			"prune":    false, // GT16: pruning disabled on the self-source
			"wait":     true,
			"timeout":  "5m",
			"path":     "./",
			"sourceRef": map[string]any{
				"kind": "OCIRepository",
				"name": name,
			},
		},
	}}
	return []*unstructured.Unstructured{repo, kust}, nil
}
