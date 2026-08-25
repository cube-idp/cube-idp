package bootstrap

import (
	"context"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
)

// FieldManager is the server-side-apply field manager bootstrap owns the
// objects it installs under.
const FieldManager = "cube-idp"

// cluster is the narrow slice of the Kubernetes API bootstrap uses: server-side
// apply an object, and read one back. Defined consumer-side and satisfied by a
// thin adapter over the injected client-go dynamic.Interface + RESTMapper — and
// by a hand-rolled fake in tests, because the client-go fake dynamic client
// cannot model server-side apply for unstructured objects. get serves the
// apply-path wait (kind-set objects bootstrap just applied, so a mapping miss
// is coded and terminal); live serves the reconciliation waits, where a miss
// may mean the kind is still arriving — it re-consults discovery on every call
// and returns raw errors so the poll loop can classify transient conditions
// as pending.
type cluster interface {
	apply(ctx context.Context, obj *unstructured.Unstructured) error
	get(ctx context.Context, obj *unstructured.Unstructured) (*unstructured.Unstructured, error)
	live(ctx context.Context, obj *unstructured.Unstructured) (*unstructured.Unstructured, error)
}

// resettableRESTMapper is the capability bootstrap expects of the injected
// meta.RESTMapper beyond mapping: a discovery-cached mapper whose cache can be
// invalidated with Reset, so kinds registered by CRDs bootstrap has just
// applied resolve on retry. Declared consumer-side, where it is consumed; the
// memory-cached RESTMapper internal/kube constructs provides it. A mapper
// without it degrades loudly, not silently: the retry is skipped and the
// mapping miss surfaces as CUBE-BST-003.
type resettableRESTMapper interface {
	Reset()
}

// Applier installs objects with server-side apply and waits for the bootstrap
// kind-set to become ready. It runs against injected client-go interfaces — the
// CLI edge constructs them (via internal/kube) and passes them in, so this
// domain never imports internal/kube.
type Applier struct {
	k        cluster
	interval time.Duration
	invNS    string
}

// NewApplier builds an Applier over an injected dynamic client and REST
// mapper. The mapper is expected to additionally implement Reset() so CRs of
// just-installed CRDs can map after a discovery refresh. inventoryNamespace
// is where the bootstrap inventory ConfigMap records — an injected placement
// fact (the invariant substrate namespace), not something this domain derives.
func NewApplier(dyn dynamic.Interface, mapper meta.RESTMapper, inventoryNamespace string) *Applier {
	return &Applier{
		k:        &dynamicCluster{dyn: dyn, mapper: mapper},
		interval: pollInterval,
		invNS:    inventoryNamespace,
	}
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

// live reads an object's current state for the reconciliation waits. Unlike
// get it returns raw errors: a mapping miss (after the same discovery refresh
// and retry, re-run on every poll because the kind may still be arriving
// through the source) or a NotFound is the poll loop's to classify as
// pending, never a coded terminal error here.
func (c *dynamicCluster) live(ctx context.Context, obj *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	rc, err := c.rawResourceFor(obj)
	if err != nil {
		return nil, err
	}
	return rc.Get(ctx, obj.GetName(), metav1.GetOptions{})
}

// resourceFor maps an object's GroupVersionKind to a scoped dynamic resource
// client, wrapping an unresolvable kind as CUBE-BST-003 (the apply path's
// terminal semantics).
func (c *dynamicCluster) resourceFor(obj *unstructured.Unstructured) (dynamic.ResourceInterface, error) {
	rc, err := c.rawResourceFor(obj)
	if err != nil {
		return nil, newMappingError(obj, err)
	}
	return rc, nil
}

// rawResourceFor maps an object's GroupVersionKind to a namespace-scoped (or
// cluster-scoped) dynamic resource client, returning the raw mapper error on a
// miss. The mapper is a discovery cache that may have been primed before the
// engine CRDs were installed; on a miss it is refreshed and consulted once
// more so kinds registered by just-applied CRDs map.
func (c *dynamicCluster) rawResourceFor(obj *unstructured.Unstructured) (dynamic.ResourceInterface, error) {
	gvk := obj.GroupVersionKind()
	mapping, err := c.mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		if r, ok := c.mapper.(resettableRESTMapper); ok {
			r.Reset()
			mapping, err = c.mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
		}
		if err != nil {
			return nil, err
		}
	}
	if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
		return c.dyn.Resource(mapping.Resource).Namespace(obj.GetNamespace()), nil
	}
	return c.dyn.Resource(mapping.Resource), nil
}
