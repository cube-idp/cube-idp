// Package cluster defines the ClusterProvider seam (spec §4.1).
// Implementations are compiled in — no plugin protocol (D8).
package cluster

import (
	"context"
	"fmt"

	"github.com/rafpe/cube-idp/internal/cluster/k3dp"
	"github.com/rafpe/cube-idp/internal/cluster/kindp"
	"github.com/rafpe/cube-idp/internal/config"
	"github.com/rafpe/cube-idp/internal/diag"
	"github.com/rafpe/cube-idp/internal/kube"
)

// GatewayNodePort is the node port every cluster-creating provider must map
// the host gateway port onto; the traefik pack's service pins the same value.
// Aliased from config.GatewayNodePort (not redefined here): kindp and k3dp
// cannot import this package without an import cycle (this package's New
// factory imports both of them), so the single source of truth lives in
// internal/config, which every party already imports.
const GatewayNodePort = config.GatewayNodePort

// Provider seam defines the interface for all cluster implementations.
type Provider interface {
	Ensure(ctx context.Context, name string, spec config.ClusterSpec) (*kube.Conn, error)
	Delete(ctx context.Context, name string) error
	Exists(ctx context.Context, name string) (bool, error)
	Kubeconfig(ctx context.Context, name string) ([]byte, error)
	Diagnose(ctx context.Context, name string) []diag.Finding
}

// ImageLoader is an optional capability of cluster-creating providers: load
// per-image tar archives (Task 6's bundle format — single-image OCI-layout
// tars) directly into the cluster nodes' container runtime, so pods start
// from node-local images with no registry pull. `up --bundle` requires it;
// kindp and k3dp implement it, `existing` does not (CUBE-7005) — up.Run
// type-asserts the provider to *cluster.ImageLoader and fails fast before any
// cluster mutation when the assertion misses.
//
// The assertions live in this package (not in kindp/k3dp) for the same reason
// the Provider conformance check does: internal/cluster's New factory imports
// both provider packages, so a reverse import for a `var _` in kindp/k3dp
// would be a cycle.
type ImageLoader interface {
	// LoadImages loads every image in imageTars (original ref -> bundle tar
	// path, from bundle.Opened.ImageTars) into the named cluster's nodes.
	// A failure loading an image wraps as CUBE-7006 naming the offending
	// image; a failure just discovering the cluster's nodes wraps as CUBE-7002
	// (produce-side: the cluster/runtime itself is unreachable).
	LoadImages(ctx context.Context, name string, imageTars map[string]string) error
}

var (
	_ ImageLoader = (*kindp.Kind)(nil)
	_ ImageLoader = (*k3dp.K3d)(nil)
)

// New factory returns a Provider for the given cluster spec.
// It returns CUBE-1001 if the provider is unknown.
func New(spec config.ClusterSpec, gw config.GatewaySpec) (Provider, error) {
	switch spec.Provider {
	case "kind":
		return newKind(gw), nil
	case "k3d":
		return k3dp.New(gw), nil
	case "existing":
		return &existing{}, nil
	default:
		return nil, diag.New(diag.CodeClusterTypeUnknown,
			fmt.Sprintf("unknown cluster provider %q", spec.Provider),
			"use provider: kind, k3d, or existing")
	}
}

// newKind returns the kind ClusterProvider implementation.
func newKind(gw config.GatewaySpec) Provider {
	return kindp.New(gw)
}
