// Package oci pushes rendered packs as Flux-compatible OCI artifacts to the
// in-cluster zot registry (spec §4, "OCI push").
//
// Implementation note (Task 9): the brief's sketch used
// fluxcd/pkg/oci/client.NewClient(...); the installed version
// (github.com/fluxcd/pkg/oci v0.69.0) puts the client directly in package
// oci (import path github.com/fluxcd/pkg/oci, no /client suffix) and
// NewClient takes []crane.Option rather than a wrapped "DefaultOptions()"
// struct. This file matches that real API. zot is plain HTTP everywhere in
// this design (see registry.InClusterURL / OCIRepository.spec.insecure), so
// the client always carries crane.Insecure, which makes
// github.com/google/go-containerregistry/pkg/name parse the ref with an
// "http://" scheme instead of "https://" — this is what makes pushing to
// the 127.0.0.1 zot port-forward tunnel work without TLS.
//
// Artifact format: Client.Push defaults to LayerTypeTarball, which produces
// a single gzipped-tar layer with media type
// application/vnd.cncf.flux.content.v1.tar+gzip. Flux's OCIRepository
// CustomResourceDefinition (embedded at
// internal/engine/flux/manifests/install.yaml, source.toolkit.fluxcd.io/v1)
// documents spec.layerSelector as: "When not specified, the first layer
// found in the artifact is selected" and extracted — so leaving
// layerSelector unset (as flux.Deliver does) is exactly compatible with
// this push shape; no custom media type or layerSelector wiring is needed.
package oci

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	fluxoci "github.com/fluxcd/pkg/oci"
	"github.com/google/go-containerregistry/pkg/crane"
	"sigs.k8s.io/yaml"

	"github.com/rafpe/cube-idp/internal/diag"
	"github.com/rafpe/cube-idp/internal/engine"
	"github.com/rafpe/cube-idp/internal/pack"
)

// PushRendered writes r.Objects as one all.yaml into a tmp dir and pushes it
// as a Flux-compatible OCI artifact to <registryAddr>/packs/<name>:<version>.
// registryAddr is a plain host:port (no scheme) — e.g. the 127.0.0.1
// port-forward tunnel to the in-cluster zot registry.
func PushRendered(ctx context.Context, r *pack.Rendered, registryAddr string) (engine.ArtifactRef, error) {
	dir, err := os.MkdirTemp("", "cube-idp-artifact-*")
	if err != nil {
		return engine.ArtifactRef{}, diag.Wrap(err, diag.CodeOCIPushFail, "cannot create temp dir for artifact staging",
			"check disk space and permissions on the system temp directory")
	}
	defer os.RemoveAll(dir)

	var buf []byte
	for _, o := range r.Objects {
		y, err := yaml.Marshal(o.Object)
		if err != nil {
			return engine.ArtifactRef{}, diag.Wrap(err, diag.CodeOCIPushFail, "cannot marshal rendered object to YAML",
				"this is a cube-idp bug — please report it")
		}
		buf = append(buf, []byte("---\n")...)
		buf = append(buf, y...)
	}
	if err := os.WriteFile(filepath.Join(dir, "all.yaml"), buf, 0o644); err != nil {
		return engine.ArtifactRef{}, diag.Wrap(err, diag.CodeOCIPushFail, "cannot write staged artifact",
			"check disk space and permissions on the system temp directory")
	}

	ref := engine.ArtifactRef{Repo: "packs/" + r.Name, Tag: r.Version}
	url := fmt.Sprintf("%s/%s:%s", registryAddr, ref.Repo, ref.Tag)

	c := fluxoci.NewClient([]crane.Option{crane.Insecure}) // zot is plain HTTP; see package doc above
	if _, err := c.Push(ctx, url, dir, fluxoci.WithPushMetadata(fluxoci.Metadata{
		Source: "cube-idp", Revision: r.Version,
	})); err != nil {
		return engine.ArtifactRef{}, diag.Wrap(err, diag.CodeOCIPushFail,
			fmt.Sprintf("failed to push pack %q to the in-cluster registry", r.Name),
			"re-run `cube-idp up`; check that the zot pod is running")
	}
	return ref, nil
}
