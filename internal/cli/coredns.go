package cli

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	"github.com/cube-idp/cube-idp/internal/gateway"
)

// Where the cluster's DNS configuration lives, and how many times the edge
// re-reads it before giving up. The ConfigMap is kubeadm's, not the cube's:
// it is spliced, never owned, and never inventory-recorded (the inventory is
// a deletion seed, and a system object must never be seeded for deletion).
const (
	corednsNamespace = "kube-system"
	corednsName      = "coredns"
	corefileKey      = "Corefile"
	spliceAttempts   = 5
)

// configMapGVR addresses core ConfigMaps, written out and built per call for
// the reasons secretGVR is: a core kind needs no discovery round-trip, and a
// package-level value would be mutable state outside main.
func configMapGVR() schema.GroupVersionResource {
	return schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}
}

// configMapClient is the two-method slice of the dynamic client the splice
// needs: read the live object, write it back. It is consumer-side and narrow
// on purpose — the write is an optimistic Update carrying the read
// resourceVersion, never a read-derived SSA write, which could clobber a
// concurrent Corefile change (docs/domains/gateway.md, safety envelope clause
// 3). A namespaced dynamic resource satisfies it as it stands.
type configMapClient interface {
	Get(ctx context.Context, name string, opts metav1.GetOptions, subresources ...string) (*unstructured.Unstructured, error)
	Update(ctx context.Context, obj *unstructured.Unstructured, opts metav1.UpdateOptions, subresources ...string) (*unstructured.Unstructured, error)
}

// coreDNSSplicer makes one cube's hostnames resolve inside the cluster. The
// post-install stage takes this act rather than a client, so what gates the
// splice can be asserted without standing up a dynamic client.
type coreDNSSplicer func(ctx context.Context, cubeName, domain string) error

// spliceCubeDomain binds the live client into a splicer. The splice takes its
// own short budget derived from the caller's context rather than the install
// budget — the same reason the CA read takes one.
func spliceCubeDomain(dyn dynamic.Interface) coreDNSSplicer {
	return func(ctx context.Context, cubeName, domain string) error {
		ctx, cancel := context.WithTimeout(ctx, edgeIOTimeout)
		defer cancel()
		return spliceCoreDNS(ctx, dyn.Resource(configMapGVR()).Namespace(corednsNamespace), cubeName, domain)
	}
}

// spliceCoreDNS performs the read-modify-write that puts this cube's rewrite
// block into the live Corefile: read, splice, write back under the read
// resourceVersion, and on a conflict start over from a fresh read. Re-reading
// and re-splicing on every attempt is what keeps a concurrent foreign edit —
// another cube's block included — intact: the splice is idempotent and
// preserving, so it is applied to whatever the other writer left.
//
// Retries are bounded and hand-rolled: five attempts, then the last conflict
// surfaces. A failure here fails the bootstrap, never a silent degrade — a
// cube whose in-cluster names do not resolve is not a bootstrapped cube.
func spliceCoreDNS(ctx context.Context, cms configMapClient, cubeName, domain string) error {
	var conflict error
	for range spliceAttempts {
		obj, err := splicedConfigMap(ctx, cms, cubeName, domain)
		if err != nil {
			return err
		}
		_, err = cms.Update(ctx, obj, metav1.UpdateOptions{})
		switch {
		case err == nil:
			return nil
		case !apierrors.IsConflict(err):
			return fmt.Errorf("update the %s/%s ConfigMap: %w", corednsNamespace, corednsName, err)
		}
		conflict = err
	}
	return fmt.Errorf("update the %s/%s ConfigMap: every one of %d attempts lost a concurrent write: %w",
		corednsNamespace, corednsName, spliceAttempts, conflict)
}

// splicedConfigMap reads the live ConfigMap and returns it carrying the
// spliced Corefile, ready to be written back under the resourceVersion the
// read gave it.
//
// A missing data.Corefile key is deliberately not judged here: what was read
// — the empty string included — goes to the pure splice, which owns the
// structural verdict (CUBE-GWY-004, "no `.:53` server block to splice into").
// One structural authority, not two.
func splicedConfigMap(ctx context.Context, cms configMapClient, cubeName, domain string) (*unstructured.Unstructured, error) {
	obj, err := cms.Get(ctx, corednsName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("read the %s/%s ConfigMap: %w", corednsNamespace, corednsName, err)
	}
	corefile, _, err := unstructured.NestedString(obj.Object, "data", corefileKey)
	if err != nil {
		return nil, fmt.Errorf("read the %s/%s %s: %w", corednsNamespace, corednsName, corefileKey, err)
	}
	spliced, err := gateway.CorefileSplice(corefile, cubeName, domain)
	if err != nil {
		return nil, err
	}
	if err := unstructured.SetNestedField(obj.Object, spliced, "data", corefileKey); err != nil {
		return nil, fmt.Errorf("set the %s/%s %s: %w", corednsNamespace, corednsName, corefileKey, err)
	}
	return obj, nil
}
