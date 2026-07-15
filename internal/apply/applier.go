// Package apply wraps fluxcd/pkg/ssa: server-side apply with field manager
// "cube-idp", kstatus waits with hard deadlines, and a ConfigMap inventory
// that powers down/prune (spec §4.1 Applier).
package apply

import (
	"context"
	"time"

	"github.com/fluxcd/cli-utils/pkg/kstatus/polling"
	"github.com/fluxcd/cli-utils/pkg/object"
	"github.com/fluxcd/pkg/ssa"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/rafpe/cube-idp/internal/diag"
)

const (
	FieldManager    = "cube-idp"
	CubeLabel       = "cube-idp.dev/cube"
	PruneAnnotation = "cube-idp.dev/prune" // value "disabled" opts out
	SystemNamespace = "cube-idp-system"
)

// Applier drives server-side apply for a single cube's manifests, using
// fluxcd/pkg/ssa's ResourceManager for the diff/apply/wait machinery and an
// in-cluster ConfigMap inventory (see inventory.go) to power prune on down.
type Applier struct {
	rm   *ssa.ResourceManager
	c    client.Client
	cube string
}

// New builds an Applier bound to cfg, scoped to the named cube. All objects
// passed through Apply are labeled cube-idp.dev/cube=cubeName and owned by
// field manager "cube-idp".
func New(cfg *rest.Config, cubeName string) (*Applier, error) {
	c, err := client.New(cfg, client.Options{})
	if err != nil {
		return nil, diag.Wrap(err, diag.CodeApplyClientBuild, "cannot build cluster client", "check kubeconfig and connectivity")
	}
	poller := polling.NewStatusPoller(c, c.RESTMapper(), polling.Options{})
	rm := ssa.NewResourceManager(c, poller, ssa.Owner{Field: FieldManager, Group: "cube-idp.dev"})
	return &Applier{rm: rm, c: c, cube: cubeName}, nil
}

// NewWithClient builds an Applier around a caller-supplied controller-runtime
// client instead of dialing cfg itself. Production never calls this (New is
// the only real entry point); it exists so tests can wrap a real envtest
// client in sigs.k8s.io/controller-runtime/pkg/client/interceptor to rig a
// specific call (e.g. Update) to fail, exercising diag-wrap paths like
// engine.Poke's CUBE-3008 without a fake Kubernetes API.
func NewWithClient(c client.Client, cubeName string) *Applier {
	poller := polling.NewStatusPoller(c, c.RESTMapper(), polling.Options{})
	rm := ssa.NewResourceManager(c, poller, ssa.Owner{Field: FieldManager, Group: "cube-idp.dev"})
	return &Applier{rm: rm, c: c, cube: cubeName}
}

// Client returns the underlying controller-runtime client, reused by
// status/get-secrets commands.
func (a *Applier) Client() client.Client { return a.c }

// Cube returns the cube name this Applier is scoped to, used by
// engine.Health implementations to filter their component list.
func (a *Applier) Cube() string { return a.cube }

// label sets cube-idp.dev/cube=<cubeName> on every object, mutating objs in
// place. Both Apply and Diff label objects identically before handing them
// to the ResourceManager, so the cube label never shows up as drift in a
// server-side diff.
func (a *Applier) label(objs []*unstructured.Unstructured) {
	for _, o := range objs {
		labels := o.GetLabels()
		if labels == nil {
			labels = map[string]string{}
		}
		labels[CubeLabel] = a.cube
		o.SetLabels(labels)
	}
}

// Apply labels every object cube-idp.dev/cube=<cubeName>, server-side
// applies them all with field manager "cube-idp", and, if wait is true,
// blocks until kstatus reports every object ready or timeout elapses.
// A timeout produces CUBE-2001 wrapping the per-object status summary.
//
// Note: Apply mutates the passed objects in place — the cube label is set
// on each element of objs as a side effect; the slice is not copied.
func (a *Applier) Apply(ctx context.Context, objs []*unstructured.Unstructured, wait bool, timeout time.Duration) error {
	a.label(objs)
	if _, err := a.rm.ApplyAllStaged(ctx, objs, ssa.DefaultApplyOptions()); err != nil {
		return diag.Wrap(err, diag.CodeApplyFailed, "server-side apply failed", "inspect the object in the error and re-run `cube-idp up`")
	}
	if !wait {
		return nil
	}
	set := object.UnstructuredSetToObjMetadataSet(objs)
	if err := a.rm.WaitForSet(set, ssa.WaitOptions{Interval: 2 * time.Second, Timeout: timeout}); err != nil {
		return diag.Wrap(err, diag.CodeApplyWaitTimeout, "timed out waiting for resources to become ready",
			"re-run `cube-idp up` (idempotent); if it persists, inspect the resources named above with kubectl")
	}
	return nil
}
