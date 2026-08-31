package e2e

import (
	"context"
	"crypto/rand"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	v1alpha1 "github.com/cube-idp/cube-idp/api/config/v1alpha1"
	"github.com/cube-idp/cube-idp/internal/bootstrap"
	"github.com/cube-idp/cube-idp/internal/ca"
	"github.com/cube-idp/cube-idp/internal/engine/substrate"
	"github.com/cube-idp/cube-idp/internal/gateway"
)

// The kubeadm-owned object the cube splices but never owns.
const (
	corednsNamespace = "kube-system"
	corednsName      = "coredns"
	corefileKey      = "Corefile"
)

// The kinds the gateway fabric is asserted through.
var (
	namespaceGVR   = schema.GroupVersionResource{Version: "v1", Resource: "namespaces"}
	serviceGVR     = schema.GroupVersionResource{Version: "v1", Resource: "services"}
	secretGVR      = schema.GroupVersionResource{Version: "v1", Resource: "secrets"}
	crdGVR         = schema.GroupVersionResource{Group: "apiextensions.k8s.io", Version: "v1", Resource: "customresourcedefinitions"}
	helmReleaseGVR = schema.GroupVersionResource{Group: "helm.toolkit.fluxcd.io", Version: "v2", Resource: "helmreleases"}
	gatewayGVR     = schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "gateways"}
)

// gatewayPrerequisites composes the four default prerequisite units the way
// the CLI edge does. It is composed here rather than called there for the
// reason the engine content already is: the edge's resolution is unexported
// in package cli, and e2e must not be the reason it is exported. What this
// run covers is therefore the domain objects, the bootstrap machinery, and
// the splice against a real kubeadm Corefile; the resolution function itself
// is covered by the hermetic table in internal/cli.
func gatewayPrerequisites(t *testing.T, cube, domain string) []bootstrap.Unit {
	t.Helper()
	crds, err := gateway.CRDsPackObjects()
	if err != nil {
		t.Fatalf("gateway.CRDsPackObjects: %v", err)
	}
	ensured, err := ca.Ensure(ca.EnsureRequest{MintRequest: ca.MintRequest{
		CubeName: cube, Domain: domain, Now: time.Now(), Rand: rand.Reader,
	}})
	if err != nil {
		t.Fatalf("ca.Ensure: %v", err)
	}
	secrets := ca.SecretObjects(ca.SecretPlacement{
		Namespace:  gateway.Namespace,
		CASecret:   gateway.CASecretName,
		LeafSecret: gateway.LeafSecretName,
	}, ensured)
	pair := gateway.HelmPairObjects()
	for _, o := range pair {
		o.SetNamespace(gateway.Namespace)
	}
	pair = append(pair, gateway.GatewayObject(domain))
	return []bootstrap.Unit{
		bootstrap.NewRawUnit(v1alpha1.PrerequisiteGatewayPlatform, gateway.PlatformObjects()),
		bootstrap.NewRawUnit(v1alpha1.PrerequisiteGatewayAPICRDs, crds),
		bootstrap.NewInertUnit(v1alpha1.PrerequisiteCASecrets, secrets),
		bootstrap.NewCRUnit(v1alpha1.PrerequisiteTraefikGateway, pair, gatewayUnitJudge),
	}
}

// gatewayUnitJudge mirrors the edge's composed judge: the cube-authored
// Gateway passes ungated (its readiness is not gated in M11), everything else
// goes to the domain predicate.
func gatewayUnitJudge(obj *unstructured.Unstructured) (bool, string, error) {
	if obj.GetAPIVersion() == gateway.GatewayAPIVersion && obj.GetKind() == "Gateway" &&
		obj.GetName() == gateway.Name && obj.GetNamespace() == gateway.Namespace {
		return true, "", nil
	}
	return gateway.Reconciled(obj)
}

// assertGatewayFabric checks what InstallEngine established: the namespace is
// Active, the Gateway API kinds are served, both Secrets are TLS Secrets, the
// implementation reconciled, the emitted Gateway exists, and the stable name
// fronts it.
func assertGatewayFabric(ctx context.Context, t *testing.T, dyn dynamic.Interface, domain string) {
	t.Helper()
	ns, err := dyn.Resource(namespaceGVR).Get(ctx, gateway.Namespace, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("gateway namespace: %v", err)
	}
	if phase, _, _ := unstructured.NestedString(ns.Object, "status", "phase"); phase != "Active" {
		t.Errorf("gateway namespace phase = %q, want Active", phase)
	}
	if _, err := dyn.Resource(crdGVR).Get(ctx, "gateways.gateway.networking.k8s.io", metav1.GetOptions{}); err != nil {
		t.Errorf("Gateway CRD not established: %v", err)
	}
	for _, name := range []string{gateway.CASecretName, gateway.LeafSecretName} {
		s, err := dyn.Resource(secretGVR).Namespace(gateway.Namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			t.Errorf("secret %s: %v", name, err)
			continue
		}
		if kind, _, _ := unstructured.NestedString(s.Object, "type"); kind != "kubernetes.io/tls" {
			t.Errorf("secret %s type = %q, want kubernetes.io/tls", name, kind)
		}
	}
	hr, err := dyn.Resource(helmReleaseGVR).Namespace(gateway.Namespace).
		Get(ctx, gateway.ImplementationID, metav1.GetOptions{})
	if err != nil {
		t.Errorf("HelmRelease: %v", err)
	} else if !readyCondition(hr) {
		t.Error("the HelmRelease is not Ready, but its unit's wait returned")
	}
	assertGatewayAndService(ctx, t, dyn, domain)
}

// assertGatewayAndService checks the two cube-authored objects that carry the
// one platform identity: the emitted Gateway's listener covers the cube's
// wildcard, and the stable Service is the ExternalName the rewrite targets.
func assertGatewayAndService(ctx context.Context, t *testing.T, dyn dynamic.Interface, domain string) {
	t.Helper()
	gw, err := dyn.Resource(gatewayGVR).Namespace(gateway.Namespace).Get(ctx, gateway.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Gateway: %v", err)
	}
	listeners, _, _ := unstructured.NestedSlice(gw.Object, "spec", "listeners")
	if len(listeners) != 1 {
		t.Fatalf("Gateway has %d listeners, want 1", len(listeners))
	}
	listener, _ := listeners[0].(map[string]any)
	if host, _, _ := unstructured.NestedString(listener, "hostname"); host != "*."+domain {
		t.Errorf("listener hostname = %q, want *.%s", host, domain)
	}
	svc, err := dyn.Resource(serviceGVR).Namespace(gateway.Namespace).Get(ctx, gateway.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("stable Service: %v", err)
	}
	if kind, _, _ := unstructured.NestedString(svc.Object, "spec", "type"); kind != "ExternalName" {
		t.Errorf("stable Service type = %q, want ExternalName", kind)
	}
}

// assertInventoryCovers checks that the cumulative inventory names every
// prerequisite object — the deletion seed a future `down` reads.
func assertInventoryCovers(ctx context.Context, t *testing.T, dyn dynamic.Interface) {
	t.Helper()
	inv, err := dyn.Resource(configMap).Namespace(substrate.Namespace).
		Get(ctx, bootstrap.InventoryName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("inventory: %v", err)
	}
	data, _, _ := unstructured.NestedStringMap(inv.Object, "data")
	recorded := strings.Join(mapValues(data), "\n")
	for _, name := range []string{gateway.Namespace, gateway.CASecretName, gateway.LeafSecretName, gateway.Name} {
		if !strings.Contains(recorded, name) {
			t.Errorf("the inventory does not name %s", name)
		}
	}
	if strings.Contains(recorded, corednsName) {
		t.Error("the inventory names the CoreDNS ConfigMap; a system object must never be seeded for deletion")
	}
}

// mapValues returns a map's values, order unimportant — the inventory is
// searched, not compared.
func mapValues(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	return out
}

// spliceCoreDNSHere performs the edge's read-modify-write against the live
// kubeadm ConfigMap and asserts the block landed with everything unmarked
// preserved. The optimistic-concurrency retry itself is covered hermetically
// in internal/cli; nothing else writes this object during the run.
//
// The in-cluster resolution probe (`dig app.<domain>` from a pod, expecting
// the stable Service) is the contract's stated next e2e increment: it needs
// pod exec plumbing this file deliberately does not grow today.
func spliceCoreDNSHere(ctx context.Context, t *testing.T, dyn dynamic.Interface, cube, domain string) {
	t.Helper()
	cms := dyn.Resource(configMap).Namespace(corednsNamespace)
	obj, err := cms.Get(ctx, corednsName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("read the CoreDNS ConfigMap: %v", err)
	}
	before, _, _ := unstructured.NestedString(obj.Object, "data", corefileKey)
	spliced, err := gateway.CorefileSplice(before, cube, domain)
	if err != nil {
		t.Fatalf("CorefileSplice: %v", err)
	}
	if err := unstructured.SetNestedField(obj.Object, spliced, "data", corefileKey); err != nil {
		t.Fatalf("set the Corefile: %v", err)
	}
	if _, err := cms.Update(ctx, obj, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("update the CoreDNS ConfigMap: %v", err)
	}

	live, err := cms.Get(ctx, corednsName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("re-read the CoreDNS ConfigMap: %v", err)
	}
	after, _, _ := unstructured.NestedString(live.Object, "data", corefileKey)
	if !strings.Contains(after, "# cube-idp:begin "+cube) {
		t.Error("the live Corefile carries no marker block")
	}
	if !strings.Contains(after, gateway.ServiceFQDN) {
		t.Error("the live rewrite does not target the stable gateway Service")
	}
	for _, line := range strings.Split(before, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" && !strings.Contains(after, trimmed) {
			t.Errorf("the splice lost the unmarked line %q", trimmed)
		}
	}
}
