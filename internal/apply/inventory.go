package apply

import (
	"context"
	"encoding/json"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/fluxcd/cli-utils/pkg/object"

	"github.com/rafpe/cube-idp/internal/diag"
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
		return diag.Wrap(err, "CUBE-2004", "cannot encode inventory", "this is a bug; please report it")
	}

	nsObj := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: SystemNamespace}}
	if err := a.c.Create(ctx, nsObj); err != nil && !apierrors.IsAlreadyExists(err) {
		return diag.Wrap(err, "CUBE-2004", "cannot create system namespace", "check RBAC on namespace cube-idp-system")
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
			return diag.Wrap(err, "CUBE-2004", "cannot write inventory", "check RBAC on namespace cube-idp-system")
		}
		return nil
	}
	if getErr != nil {
		return diag.Wrap(getErr, "CUBE-2004", "cannot read inventory", "check RBAC on namespace cube-idp-system")
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
		return diag.Wrap(err, "CUBE-2004", "cannot write inventory", "check RBAC on namespace cube-idp-system")
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
		return nil, diag.Wrap(err, "CUBE-2004", "cannot read inventory", "check RBAC on namespace cube-idp-system")
	}
	var strs []string
	if err := json.Unmarshal([]byte(cm.Data["inventory"]), &strs); err != nil {
		return nil, diag.Wrap(err, "CUBE-2004", "inventory is corrupt", "delete the ConfigMap and re-run `cube-idp up` to rebuild it")
	}
	refs := make([]object.ObjMetadata, 0, len(strs))
	for _, s := range strs {
		ref, err := object.ParseObjMetadata(s)
		if err != nil {
			return nil, diag.Wrap(err, "CUBE-2004", "inventory is corrupt", "delete the ConfigMap and re-run `cube-idp up` to rebuild it")
		}
		refs = append(refs, ref)
	}
	return refs, nil
}

// DeleteAll deletes every object recorded in the inventory, in reverse apply
// order, skipping any object annotated cube-idp.dev/prune=disabled, then
// removes the inventory ConfigMap itself.
func (a *Applier) DeleteAll(ctx context.Context, timeout time.Duration) error {
	// timeout is reserved for a future kstatus wait-for-termination pass.
	// envtest runs without a namespace controller, so Namespace deletions
	// never actually complete there; a hard wait on all deleted kinds would
	// make DeleteAll hang forever under test. Deletes below are issued
	// synchronously (deletionTimestamp is set immediately by the API
	// server) which is sufficient for the "gone"/"keep" contract this
	// method promises today.
	_ = timeout

	refs, err := a.LoadInventory(ctx)
	if err != nil {
		return err
	}

	var deletable []*unstructured.Unstructured
	for _, ref := range refs {
		obj, getErr := a.getByRef(ctx, ref)
		if getErr != nil {
			continue // already gone
		}
		if obj.GetAnnotations()[PruneAnnotation] == "disabled" {
			continue
		}
		deletable = append(deletable, obj)
	}

	// Reverse order: objects applied later (e.g. dependents) are deleted
	// first, mirroring flux's teardown ordering.
	for i := len(deletable) - 1; i >= 0; i-- {
		if err := a.c.Delete(ctx, deletable[i]); err != nil && !apierrors.IsNotFound(err) {
			return diag.Wrap(err, "CUBE-2005", "cannot delete resource during prune", "inspect the resource named above with kubectl and delete it manually if needed")
		}
	}

	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: a.inventoryName(), Namespace: SystemNamespace}}
	if err := a.c.Delete(ctx, cm); err != nil && !apierrors.IsNotFound(err) {
		return diag.Wrap(err, "CUBE-2004", "cannot delete inventory", "check RBAC on namespace cube-idp-system")
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
