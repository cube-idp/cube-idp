package apply

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/fluxcd/cli-utils/pkg/object"

	"github.com/cube-idp/cube-idp/internal/diag"
)

func (a *Applier) inventoryName() string { return "cube-idp-inventory-" + a.cube }

// RecordInventory merges refs for objs into the ConfigMap
// cube-idp-inventory-<cube> in namespace cube-idp-system, so DeleteAll can
// later find and prune every object cube-idp has ever applied for this cube.
func (a *Applier) RecordInventory(ctx context.Context, objs []*unstructured.Unstructured) error {
	refs := object.UnstructuredSetToObjMetadataSet(objs)
	existing, err := a.LoadInventory(ctx)
	if err != nil {
		return err
	}
	merged := object.ObjMetadataSet(existing).Union(refs)

	strs := make([]string, 0, len(merged))
	for _, ref := range merged {
		strs = append(strs, ref.String())
	}
	payload, err := json.Marshal(strs)
	if err != nil {
		return diag.Wrap(err, diag.CodeInventoryFailed, "cannot encode inventory", "this is a bug; please report it")
	}

	nsObj := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: SystemNamespace}}
	if err := a.c.Create(ctx, nsObj); err != nil && !apierrors.IsAlreadyExists(err) {
		return diag.Wrap(err, diag.CodeInventoryFailed, "cannot create system namespace", "check RBAC on namespace cube-idp-system")
	}

	var cm corev1.ConfigMap
	getErr := a.c.Get(ctx, client.ObjectKey{Namespace: SystemNamespace, Name: a.inventoryName()}, &cm)
	if apierrors.IsNotFound(getErr) {
		cm = corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      a.inventoryName(),
				Namespace: SystemNamespace,
				Labels:    map[string]string{CubeLabel: a.cube},
			},
			Data: map[string]string{"inventory": string(payload)},
		}
		if err := a.c.Create(ctx, &cm); err != nil {
			return diag.Wrap(err, diag.CodeInventoryFailed, "cannot write inventory", "check RBAC on namespace cube-idp-system")
		}
		return nil
	}
	if getErr != nil {
		return diag.Wrap(getErr, diag.CodeInventoryFailed, "cannot read inventory", "check RBAC on namespace cube-idp-system")
	}
	if cm.Data == nil {
		cm.Data = map[string]string{}
	}
	cm.Data["inventory"] = string(payload)
	if cm.Labels == nil {
		cm.Labels = map[string]string{}
	}
	cm.Labels[CubeLabel] = a.cube
	if err := a.c.Update(ctx, &cm); err != nil {
		return diag.Wrap(err, diag.CodeInventoryFailed, "cannot write inventory", "check RBAC on namespace cube-idp-system")
	}
	return nil
}

// LoadInventory reads the inventory ConfigMap for this cube and returns the
// object refs it tracks. A missing ConfigMap is not an error: it just means
// nothing has been applied for this cube yet.
func (a *Applier) LoadInventory(ctx context.Context) ([]object.ObjMetadata, error) {
	var cm corev1.ConfigMap
	err := a.c.Get(ctx, client.ObjectKey{Namespace: SystemNamespace, Name: a.inventoryName()}, &cm)
	if apierrors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, diag.Wrap(err, diag.CodeInventoryFailed, "cannot read inventory", "check RBAC on namespace cube-idp-system")
	}
	var strs []string
	if err := json.Unmarshal([]byte(cm.Data["inventory"]), &strs); err != nil {
		return nil, diag.Wrap(err, diag.CodeInventoryFailed, "inventory is corrupt", "delete the ConfigMap and re-run `cube-idp up` to rebuild it")
	}
	refs := make([]object.ObjMetadata, 0, len(strs))
	for _, s := range strs {
		ref, err := object.ParseObjMetadata(s)
		if err != nil {
			return nil, diag.Wrap(err, diag.CodeInventoryFailed, "inventory is corrupt", "delete the ConfigMap and re-run `cube-idp up` to rebuild it")
		}
		refs = append(refs, ref)
	}
	return refs, nil
}

// DeleteAll issues deletes for every object recorded in the inventory, in
// reverse apply order (dependents first), skipping any object annotated
// cube-idp.dev/prune=disabled, then removes the inventory ConfigMap itself.
//
// Contract notes for callers (e.g. `cube-idp down`):
//   - It does NOT wait for termination: deletes are issued synchronously and
//     return once the API server has set deletionTimestamp. The timeout
//     parameter is reserved for future wait-for-termination semantics.
//   - Inventory entries whose object is already gone, or whose kind no
//     longer exists (CRD already deleted), are skipped silently.
//   - Any other failure to inspect or delete an inventoried object (RBAC
//     denial, transient API error) is collected; DeleteAll still attempts
//     every remaining deletion, then returns all failures joined under
//     CUBE-2006. The inventory ConfigMap is kept in that case so a re-run
//     can retry the survivors — DeleteAll never returns nil after silently
//     orphaning resources it could not inspect.
func (a *Applier) DeleteAll(ctx context.Context, timeout time.Duration) error {
	// timeout is reserved for a future kstatus wait-for-termination pass.
	// envtest runs without a namespace controller, so Namespace deletions
	// never actually complete there; a hard wait on all deleted kinds would
	// make DeleteAll hang forever under test.
	_ = timeout

	refs, err := a.LoadInventory(ctx)
	if err != nil {
		return err
	}

	var deletable []*unstructured.Unstructured
	var failures []error
	for _, ref := range refs {
		obj, getErr := a.getByRef(ctx, ref)
		switch {
		case getErr == nil:
			// fall through to the prune-annotation check below
		case apierrors.IsNotFound(getErr) || meta.IsNoMatchError(getErr):
			continue // genuinely gone (object deleted, or its whole kind is)
		default:
			// RBAC denial, transient failure, ...: we cannot know whether the
			// object still exists, so surface it instead of orphaning it.
			failures = append(failures, fmt.Errorf("inspect %s: %w", ref, getErr))
			continue
		}
		if obj.GetAnnotations()[PruneAnnotation] == "disabled" {
			continue
		}
		deletable = append(deletable, obj)
	}

	// Reverse order: objects applied later (e.g. dependents) are deleted
	// first, mirroring flux's teardown ordering.
	for i := len(deletable) - 1; i >= 0; i-- {
		obj := deletable[i]
		if err := a.c.Delete(ctx, obj); err != nil && !apierrors.IsNotFound(err) {
			failures = append(failures, fmt.Errorf("delete %s %s/%s: %w",
				obj.GetKind(), obj.GetNamespace(), obj.GetName(), err))
		}
	}

	if len(failures) > 0 {
		// Keep the inventory ConfigMap so a re-run can retry the survivors.
		return diag.Wrap(errors.Join(failures...), diag.CodeApplyPruneFailed,
			"some inventoried resources could not be pruned",
			"re-run `cube-idp down`; inspect the listed resources with kubectl")
	}

	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: a.inventoryName(), Namespace: SystemNamespace}}
	if err := a.c.Delete(ctx, cm); err != nil && !apierrors.IsNotFound(err) {
		return diag.Wrap(err, diag.CodeInventoryFailed, "cannot delete inventory", "check RBAC on namespace cube-idp-system")
	}
	return nil
}

// getByRef fetches the live object identified by ref, resolving its
// preferred version through the client's RESTMapper.
func (a *Applier) getByRef(ctx context.Context, ref object.ObjMetadata) (*unstructured.Unstructured, error) {
	mapping, err := a.c.RESTMapper().RESTMapping(ref.GroupKind)
	if err != nil {
		return nil, err
	}
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(mapping.GroupVersionKind)
	key := client.ObjectKey{Namespace: ref.Namespace, Name: ref.Name}
	if err := a.c.Get(ctx, key, u); err != nil {
		return nil, err
	}
	return u, nil
}
