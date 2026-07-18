// Package tests holds cross-package smoke tests that don't belong to any
// single internal package: this file renders every starter pack end-to-end
// (pack.Fetch -> Render), the same path cube-idp's `up` orchestration
// exercises for a real cluster. P4: the starter packs live in the
// cube-idp/packs monorepo — these tests scan a local checkout of it
// (packsTree) and SKIP when none is present; the authoritative per-pack
// gate is the packs repo's own conformance harness.
package tests

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/cube-idp/cube-idp/internal/apply"
	"github.com/cube-idp/cube-idp/internal/pack"
)

// packsTree resolves the cube-idp/packs checkout's packs/ directory the
// pack-content smoke tests in this package scan. CUBE_IDP_E2E_PACKS_DIR
// (the same knob the e2e suite uses — tests/e2e/PACKS.md) overrides; unset,
// it defaults to the sibling checkout ../cube-idp-packs/packs. Tests SKIP
// when no checkout is present.
func packsTree(t *testing.T) string {
	t.Helper()
	dir := os.Getenv("CUBE_IDP_E2E_PACKS_DIR")
	if dir == "" {
		dir = filepath.Join("..", "..", "cube-idp-packs", "packs")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		t.Fatalf("resolving packs dir %q: %v", dir, err)
	}
	if st, err := os.Stat(abs); err != nil || !st.IsDir() {
		t.Skipf("no cube-idp/packs checkout at %s — clone "+
			"https://github.com/cube-idp/packs as ../cube-idp-packs or set "+
			"CUBE_IDP_E2E_PACKS_DIR to a checkout's packs/ directory "+
			"(tests/e2e/PACKS.md)", abs)
	}
	return abs
}

// namespacedKinds are kinds we know are namespace-scoped and that appear
// in the starter packs. cube-idp's delivery path (rendered objects -> OCI
// artifact -> Flux Kustomization with no targetNamespace) applies objects
// exactly as rendered, so any of these missing metadata.namespace would
// silently land in `default` — the bug class this guards against is
// vendoring an upstream bundle (like argo-cd's install.yaml) that assumes
// `kubectl apply -n <ns>` supplies the namespace externally.
var namespacedKinds = map[string]bool{
	"Deployment":     true,
	"StatefulSet":    true,
	"Service":        true,
	"ConfigMap":      true,
	"Secret":         true,
	"ServiceAccount": true,
	"Role":           true,
	"RoleBinding":    true,
	"NetworkPolicy":  true,
	"HTTPRoute":      true,
}

// TestStarterPacksRender is network-gated (helm chart pulls +
// gateway-api/argo-cd manifest parsing): it renders each starter pack with
// no user-supplied values (nil), matching how `cube-idp up` invokes packs
// with only their pack.cue defaults, and asserts each produces at least
// one object and that every known-namespaced object carries an explicit
// metadata.namespace. This is deliberately a smoke test, not a golden test
// — starter pack manifests are vendored upstream content that changes on
// every chart bump, so pinning exact output would make routine version
// bumps fail CI.
func TestStarterPacksRender(t *testing.T) {
	if testing.Short() {
		t.Skip("helm renders hit the network")
	}
	root := packsTree(t)
	for _, name := range []string{
		"traefik", "gitea", "argocd",
		"backstage", "cert-manager",
		"external-secrets", "envoy-gateway",
	} {
		dir := filepath.Join(root, name)
		p, err := pack.Fetch(context.Background(), dir, t.TempDir())
		if err != nil {
			t.Fatalf("%s: %v", dir, err)
		}
		r, err := p.Render(nil)
		if err != nil {
			t.Fatalf("%s render: %v", dir, err)
		}
		if len(r.Objects) == 0 {
			t.Fatalf("%s rendered zero objects", dir)
		}
		for _, o := range r.Objects {
			if namespacedKinds[o.GetKind()] && o.GetNamespace() == "" {
				t.Errorf("%s: %s %q has no metadata.namespace — it would land in `default` when applied",
					dir, o.GetKind(), o.GetName())
			}
		}

		// The envoy-gateway pack's gateway-helm chart ships the Gateway API
		// CRDs (and Envoy Gateway's own CRDs) under its crds/ directory, and
		// its certgen Job (which creates the TLS secret the controller
		// mounts) as a pre-install helm hook. Helm's dry-run render drops
		// both — crds/ objects from the manifest, hooks onto Release.Hooks —
		// so before the re-injection fixes (internal/pack/helm.go) this
		// render silently lacked them: `up` timed out waiting for the
		// HTTPRoute CRD, and once that was fixed, the controller pod hung on
		// FailedMount of the missing certs secret. Assert both are back.
		if name == "envoy-gateway" {
			crds := map[string]bool{}
			var certgenJob *unstructured.Unstructured
			for _, o := range r.Objects {
				if o.GetKind() == "CustomResourceDefinition" {
					crds[o.GetName()] = true
				}
				if o.GetKind() == "Job" && strings.HasSuffix(o.GetName(), "-certgen") {
					certgenJob = o
				}
			}
			if len(crds) == 0 {
				t.Errorf("%s: rendered no CustomResourceDefinition objects — gateway-helm's crds/ were dropped from the render", dir)
			}
			if !crds["httproutes.gateway.networking.k8s.io"] {
				t.Errorf("%s: rendered CRDs missing httproutes.gateway.networking.k8s.io (the Gateway API CRD `up` waits to establish); got %d CRDs", dir, len(crds))
			}
			if certgenJob == nil {
				t.Errorf("%s: rendered no certgen Job — gateway-helm's pre-install hook was dropped from the render", dir)
			} else if _, hook := certgenJob.GetAnnotations()["helm.sh/hook"]; hook {
				t.Errorf("%s: certgen Job still carries the helm.sh/hook annotation: %v", dir, certgenJob.GetAnnotations())
			}

			// R7b collision check (spec §7 risk): the name pack.cue's
			// gatewayService: declares (and the raw EnvoyProxy's
			// envoyService.name pins) MUST be free in the rendered stream —
			// no rendered v1 Service already claims it in the
			// envoy-gateway namespace. If one did, EG's generated
			// data-plane Service would collide with it exactly like the F9
			// hijack (an existing Service's selector getting overwritten),
			// just with a different colliding owner. It also pins that the
			// pack's parsed GatewayService and the manifest's EnvoyProxy
			// agree — one declared name, one place of truth.
			if p.GatewayService == nil {
				t.Fatalf("%s: pack.cue must declare gatewayService: — the CoreDNS rewrite target for this pack (R7b)", dir)
			}
			for _, o := range r.Objects {
				if o.GetKind() == "Service" && o.GetNamespace() == p.GatewayService.Namespace && o.GetName() == p.GatewayService.Name {
					t.Errorf("%s: a rendered v1 Service already claims %s/%s — that name must stay free for EG's generated data-plane Service, or the F9 hijack recurs",
						dir, p.GatewayService.Namespace, p.GatewayService.Name)
				}
			}
			epName := envoyProxyServiceName(t, dir)
			if epName != p.GatewayService.Name {
				t.Errorf("%s: EnvoyProxy envoyService.name %q does not match pack.cue's gatewayService.name %q — up's CoreDNS rewrite and the actual generated Service would disagree",
					dir, epName, p.GatewayService.Name)
			}
		}
	}
}

// envoyProxyServiceName reads packDir/manifests/10-gatewayclass.yaml (raw,
// no helm — the same file TestEnvoyGatewayPackProxyService parses) and
// returns its EnvoyProxy's spec.provider.kubernetes.envoyService.name.
func envoyProxyServiceName(t *testing.T, packDir string) string {
	t.Helper()
	raw, err := os.ReadFile(packDir + "/manifests/10-gatewayclass.yaml")
	if err != nil {
		t.Fatal(err)
	}
	objs, err := apply.ParseMultiDoc(raw)
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range objs {
		if o.GetKind() != "EnvoyProxy" {
			continue
		}
		name, _, err := unstructured.NestedString(o.Object, "spec", "provider", "kubernetes", "envoyService", "name")
		if err != nil {
			t.Fatal(err)
		}
		return name
	}
	t.Fatalf("no EnvoyProxy object in %s/manifests/10-gatewayclass.yaml", packDir)
	return ""
}

// TestEnvoyGatewayPackProxyService pins the F9-follow-up root cause found
// live on the first envoy leg (2026-07-15, fix-envoy-dbg): the pack's
// EnvoyProxy set envoyService.name to "envoy-gateway" — the exact name of
// the Envoy Gateway CONTROLLER's own Service, which every proxy's static
// bootstrap dials for xDS (envoy-gateway.envoy-gateway.svc:18000,
// hardcoded in EG v1.3.0). The generated proxy Service then overwrote that
// Service's selector to the proxy pods, the proxy's xDS dial landed on
// itself (connection refused), no listener was ever programmed, and every
// host connection to the NodePort reset — while the CONTROL plane happily
// reported Programmed=True and attachedRoutes=3. This test parses the raw
// manifest (no helm, no network) and pins three load-bearing facts:
//  1. envoyService must NOT be named "envoy-gateway" (the controller's xDS
//     Service name) — leave `name` unset so EG generates its own unique
//     proxy Service name.
//  2. externalTrafficPolicy is explicitly "Cluster": the CRD schema
//     DEFAULTS it to Local (verified against the live 1.3.0 CRD), and
//     Local drops kind's docker-proxy -> NodePort traffic unless kube-proxy
//     happens to keep the client on a pod-bearing node. Cluster trades away
//     client source-IP preservation, which cube-idp does not need locally.
//  3. The StrategicMerge patch still pins the cube-idp NodePorts
//     (30443/30080) the kind host-port mapping relies on.
func TestEnvoyGatewayPackProxyService(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(packsTree(t), "envoy-gateway", "manifests", "10-gatewayclass.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	objs, err := apply.ParseMultiDoc(raw)
	if err != nil {
		t.Fatal(err)
	}
	var ep *unstructured.Unstructured
	for _, o := range objs {
		if o.GetKind() == "EnvoyProxy" {
			ep = o
		}
	}
	if ep == nil {
		t.Fatal("no EnvoyProxy object in 10-gatewayclass.yaml")
	}
	svc, ok, err := unstructured.NestedMap(ep.Object, "spec", "provider", "kubernetes", "envoyService")
	if err != nil || !ok {
		t.Fatalf("spec.provider.kubernetes.envoyService missing: %v", err)
	}
	if name, ok := svc["name"].(string); ok && name == "envoy-gateway" {
		t.Fatal("envoyService.name must not be \"envoy-gateway\": that is the controller's xDS Service name — the generated proxy Service hijacks its selector and the proxy can never fetch config (host connections reset)")
	}
	if etp, _ := svc["externalTrafficPolicy"].(string); etp != "Cluster" {
		t.Fatalf("envoyService.externalTrafficPolicy must be explicitly \"Cluster\" (the CRD defaults to Local, which breaks kind's docker-proxy NodePort path), got %q", etp)
	}
	ports, ok, _ := unstructured.NestedSlice(ep.Object, "spec", "provider", "kubernetes", "envoyService", "patch", "value", "spec", "ports")
	if !ok {
		t.Fatal("envoyService.patch must pin the cube-idp NodePorts")
	}
	asInt := func(v any) int64 {
		switch n := v.(type) {
		case int64:
			return n
		case float64:
			return int64(n)
		default:
			t.Fatalf("unexpected numeric type %T", v)
			return 0
		}
	}
	want := map[int64]int64{8443: 30443, 8000: 30080}
	for _, p := range ports {
		m := p.(map[string]any)
		port, nodePort := asInt(m["port"]), asInt(m["nodePort"])
		if want[port] != nodePort {
			t.Fatalf("port %d pinned to nodePort %d, want %d", port, nodePort, want[port])
		}
		delete(want, port)
	}
	if len(want) != 0 {
		t.Fatalf("ports missing from the NodePort patch: %v", want)
	}
}
