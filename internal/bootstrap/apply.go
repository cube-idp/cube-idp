package bootstrap

import (
	"bytes"
	"context"
	"errors"
	"io"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/dynamic"
)

// FieldManager is the server-side-apply field manager bootstrap owns the
// objects it installs under.
const FieldManager = "cube-idp"

// cluster is the narrow slice of the Kubernetes API bootstrap uses: server-side
// apply an object, and read one back. Defined consumer-side and satisfied by a
// thin adapter over the injected client-go dynamic.Interface + RESTMapper — and
// by a hand-rolled fake in tests, because the client-go fake dynamic client
// cannot model server-side apply for unstructured objects.
type cluster interface {
	apply(ctx context.Context, obj *unstructured.Unstructured) error
	get(ctx context.Context, obj *unstructured.Unstructured) (*unstructured.Unstructured, error)
}

// Applier installs objects with server-side apply and waits for the bootstrap
// kind-set to become ready. It runs against injected client-go interfaces — the
// CLI edge constructs them (via internal/kube) and passes them in, so this
// domain never imports internal/kube.
type Applier struct {
	k        cluster
	interval time.Duration
}

// NewApplier builds an Applier over an injected dynamic client and REST mapper.
func NewApplier(dyn dynamic.Interface, mapper meta.RESTMapper) *Applier {
	return &Applier{k: &dynamicCluster{dyn: dyn, mapper: mapper}, interval: pollInterval}
}

// Apply server-side-applies each object in order under the cube-idp field
// manager, forcing conflicts — bootstrap owns what it installs.
func (a *Applier) Apply(ctx context.Context, objs []*unstructured.Unstructured) error {
	for _, obj := range objs {
		if err := a.k.apply(ctx, obj); err != nil {
			return err
		}
	}
	return nil
}

// dynamicCluster adapts the injected dynamic client + REST mapper to the
// cluster seam. It is the only code that turns a GroupVersionKind into a scoped
// dynamic resource client.
type dynamicCluster struct {
	dyn    dynamic.Interface
	mapper meta.RESTMapper
}

func (c *dynamicCluster) apply(ctx context.Context, obj *unstructured.Unstructured) error {
	rc, err := c.resourceFor(obj)
	if err != nil {
		return err
	}
	opts := metav1.ApplyOptions{FieldManager: FieldManager, Force: true}
	if _, err := rc.Apply(ctx, obj.GetName(), obj, opts); err != nil {
		return newApplyError(obj, err)
	}
	return nil
}

func (c *dynamicCluster) get(ctx context.Context, obj *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	rc, err := c.resourceFor(obj)
	if err != nil {
		return nil, err
	}
	return rc.Get(ctx, obj.GetName(), metav1.GetOptions{})
}

// resourceFor maps an object's GroupVersionKind to a namespace-scoped (or
// cluster-scoped) dynamic resource client.
func (c *dynamicCluster) resourceFor(obj *unstructured.Unstructured) (dynamic.ResourceInterface, error) {
	gvk := obj.GroupVersionKind()
	mapping, err := c.mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		// The mapper is a discovery cache that may have been primed before the
		// Flux CRDs were installed; force a refresh and retry once so kinds
		// registered by just-applied CRDs (GitRepository, Kustomization, …) map.
		if r, ok := c.mapper.(interface{ Reset() }); ok {
			r.Reset()
			mapping, err = c.mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
		}
		if err != nil {
			return nil, newMappingError(obj, err)
		}
	}
	if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
		return c.dyn.Resource(mapping.Resource).Namespace(obj.GetNamespace()), nil
	}
	return c.dyn.Resource(mapping.Resource), nil
}

// FluxObjects returns the embedded, pinned Flux install manifests parsed into
// apply-ready unstructured objects (provenance-verified via Manifests).
func FluxObjects() ([]*unstructured.Unstructured, error) {
	data, err := Manifests()
	if err != nil {
		return nil, err
	}
	return parseManifests(data)
}

// parseManifests splits a multi-document YAML stream into unstructured objects,
// skipping empty documents.
func parseManifests(data []byte) ([]*unstructured.Unstructured, error) {
	dec := utilyaml.NewYAMLOrJSONDecoder(bytes.NewReader(data), 4096)
	var objs []*unstructured.Unstructured
	for {
		m := map[string]any{}
		if err := dec.Decode(&m); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, newManifestParseError(err)
		}
		if len(m) == 0 {
			continue
		}
		objs = append(objs, &unstructured.Unstructured{Object: m})
	}
	return objs, nil
}
