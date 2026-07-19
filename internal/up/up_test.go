package up

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/rest"

	"github.com/cube-idp/cube-idp/internal/apply"
	"github.com/cube-idp/cube-idp/internal/cluster"
	"github.com/cube-idp/cube-idp/internal/config"
	"github.com/cube-idp/cube-idp/internal/diag"
	"github.com/cube-idp/cube-idp/internal/engine"
	"github.com/cube-idp/cube-idp/internal/gitea"
	"github.com/cube-idp/cube-idp/internal/kube"
	"github.com/cube-idp/cube-idp/internal/lock"
	"github.com/cube-idp/cube-idp/internal/pack"
	"github.com/cube-idp/cube-idp/internal/spoke"
	"github.com/cube-idp/cube-idp/internal/ui"
	"github.com/cube-idp/cube-idp/internal/ui/event"
)

// TestVerifyGatewayPackRef pins F11: gateway.ref silently wins over
// gateway.pack, so a mismatch between the ref'd pack's declared name and
// gateway.pack must be a typed CUBE-0008 error, not a silent wrong-gateway
// delivery. The operator's exact misconfig — `init --local` wrote
// ref=.../packs/traefik, operator edited only pack: envoy-gateway — is the
// first case.
func TestVerifyGatewayPackRef(t *testing.T) {
	cases := []struct {
		name    string
		pkName  string
		gw      config.GatewaySpec
		wantErr bool
	}{
		{
			name:    "operator misconfig: ref=traefik pack=envoy-gateway",
			pkName:  "traefik",
			gw:      config.GatewaySpec{Pack: "envoy-gateway", Ref: "/repo/packs/traefik"},
			wantErr: true,
		},
		{
			name:   "ref and pack agree",
			pkName: "envoy-gateway",
			gw:     config.GatewaySpec{Pack: "envoy-gateway", Ref: "/repo/packs/envoy-gateway"},
		},
		{
			name:   "no ref: PackRef falls back to packs/<pack>, cannot disagree",
			pkName: "traefik",
			gw:     config.GatewaySpec{Pack: "envoy-gateway"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := verifyGatewayPackRef(&pack.Pack{Name: tc.pkName}, tc.gw)
			if !tc.wantErr {
				if err != nil {
					t.Fatalf("want nil, got %v", err)
				}
				return
			}
			var de *diag.Error
			if !errors.As(err, &de) || de.Code != diag.CodeGatewayPackMismatch {
				t.Fatalf("want CUBE-0008, got %v", err)
			}
			if !strings.Contains(de.Error(), "envoy-gateway") {
				t.Fatalf("remediation should name the pack, got %q", de.Error())
			}
		})
	}
}

// TestGatewayServiceFQDNDerivation pins R7b: the CoreDNS rewrite target is
// the RESOLVED gateway pack's declared gatewayService when present (closing
// the envoy CoreDNS in-cluster gap), else the <pack>.<pack>.svc convention —
// including when gwPack itself is nil, so a caller that hasn't fetched the
// pack yet still gets today's default rather than a nil-deref.
func TestGatewayServiceFQDNDerivation(t *testing.T) {
	gw := config.GatewaySpec{Pack: "envoy-gateway"}
	declared := &pack.Pack{Name: "envoy-gateway",
		GatewayService: &pack.GatewayService{Name: "cube-idp-gateway", Namespace: "envoy-gateway"}}
	if got := gatewayServiceFQDN(gw, declared); got != "cube-idp-gateway.envoy-gateway.svc.cluster.local" {
		t.Fatalf("declared: %q", got)
	}
	plain := &pack.Pack{Name: "traefik"}
	if got := gatewayServiceFQDN(config.GatewaySpec{Pack: "traefik"}, plain); got != "traefik.traefik.svc.cluster.local" {
		t.Fatalf("default: %q", got)
	}
	if got := gatewayServiceFQDN(config.GatewaySpec{Pack: "traefik"}, nil); got != "traefik.traefik.svc.cluster.local" {
		t.Fatalf("nil pack must fall back to the default: %q", got)
	}
}

// TestMergeImagesUnion pins spec D14's lock-assembly merge: the sorted,
// deduplicated union of rendered-manifest images and a pack's declared
// (pack.cue images:) images — the pure step Run's pack loop calls per
// entry. This is the "focused unit" the D14 preparatory commit calls for,
// since Run itself needs a live cluster and isn't unit-testable here.
func TestMergeImagesUnion(t *testing.T) {
	got := mergeImages(
		[]string{"traefik:v3.1", "busybox:1.36"},
		[]string{"envoyproxy/envoy:v1.29.0", "busybox:1.36"}, // busybox is a deliberate overlap
	)
	want := []string{"busybox:1.36", "envoyproxy/envoy:v1.29.0", "traefik:v3.1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mergeImages = %v, want %v", got, want)
	}
}

// TestMergeImagesEmptyDeclared covers the common case — a pack.cue with no
// images: field (pk.Images is nil) — so the merge degenerates to the
// rendered-manifest list alone, sorted.
func TestMergeImagesEmptyDeclared(t *testing.T) {
	got := mergeImages([]string{"b:1", "a:1"}, nil)
	want := []string{"a:1", "b:1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mergeImages = %v, want %v", got, want)
	}
}

// TestResolveBundleRefs is the pure offline rule: every cube pack ref must
// resolve to a bundle-local directory (via the lock's name<->ref mapping,
// with a last-path-segment fallback for local-dir refs), or fail loudly with
// CUBE-7004 — never a silent network fetch.
func TestResolveBundleRefs(t *testing.T) {
	inBundle := map[string]string{"gitea": "/tmp/x/packs/gitea"} // pack name -> dir
	lk := &lock.File{Packs: []lock.Entry{
		{Ref: "oci://ghcr.io/cube-idp/packs/gitea:0.1.0", Name: "gitea"},
	}}
	refs := []config.PackRef{{Ref: "oci://ghcr.io/cube-idp/packs/gitea:0.1.0"}}
	resolved, err := resolveBundleRefs(refs, lk, func(name string) (string, bool) {
		d, ok := inBundle[name]
		return d, ok
	})
	if err != nil {
		t.Fatal(err)
	}
	if resolved[0].Ref != "/tmp/x/packs/gitea" {
		t.Fatalf("resolved: %+v", resolved)
	}

	// A ref absent from both the lock and the bundle is CUBE-7004.
	_, err = resolveBundleRefs(
		[]config.PackRef{{Ref: "oci://ghcr.io/cube-idp/packs/absent:1.0.0"}},
		&lock.File{},
		func(string) (string, bool) { return "", false },
	)
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != "CUBE-7004" {
		t.Fatalf("want CUBE-7004 for a ref missing from the bundle, got %v", err)
	}
}

// TestResolveBundleRefs_LocalDirFallback covers the fallback path: a ref the
// lock records verbatim as a local directory resolves by its last path
// segment (the pack name the bundle keyed it under) when no lock Ref match
// exists.
func TestResolveBundleRefs_LocalDirFallback(t *testing.T) {
	refs := []config.PackRef{{Ref: "/some/checkout/packs/traefik"}}
	resolved, err := resolveBundleRefs(refs, &lock.File{}, func(name string) (string, bool) {
		if name == "traefik" {
			return "/tmp/x/packs/traefik", true
		}
		return "", false
	})
	if err != nil {
		t.Fatal(err)
	}
	if resolved[0].Ref != "/tmp/x/packs/traefik" {
		t.Fatalf("resolved: %+v", resolved)
	}
}

// TestResolveBundleRefs_PreservesValues verifies rewriting the Ref keeps the
// pack's install-shaping fields intact — Values, extraManifests (GT15) and
// delivery (P7) — only the source location changes.
func TestResolveBundleRefs_PreservesValues(t *testing.T) {
	refs := []config.PackRef{{
		Ref:            "oci://ghcr.io/cube-idp/packs/gitea:0.1.0",
		Values:         map[string]any{"replicas": 2},
		ExtraManifests: "apiVersion: v1\nkind: ConfigMap\nmetadata: {name: seed}\n",
		Delivery:       "repo",
	}}
	lk := &lock.File{Packs: []lock.Entry{{Ref: "oci://ghcr.io/cube-idp/packs/gitea:0.1.0", Name: "gitea"}}}
	resolved, err := resolveBundleRefs(refs, lk, func(string) (string, bool) {
		return "/tmp/x/packs/gitea", true
	})
	if err != nil {
		t.Fatal(err)
	}
	if resolved[0].Values["replicas"] != 2 {
		t.Fatalf("values lost: %+v", resolved[0])
	}
	if !strings.Contains(resolved[0].ExtraManifests, "kind: ConfigMap") || resolved[0].Delivery != "repo" {
		t.Fatalf("extraManifests/delivery lost on bundle rewrite: %+v", resolved[0])
	}
}

// TestStepFetchSourcePlainOutput pins the per-pack resolved-fetch-source line
// (Task 13 review): the plain stream must name the ACTUAL source pack.Fetch
// will read — the oci:// ref online, the bundle-local dir (under a
// cube-idp-bundle-* staging dir) in --bundle mode — so the e2e's offline
// assertions ("every fetch source points into the bundle", "no fetch source
// is oci://") are falsifiable from output alone: an online run demonstrably
// prints oci:// here, a bundle run demonstrably does not.
func TestStepFetchSourcePlainOutput(t *testing.T) {
	emit := func(refs []config.PackRef) string {
		var out bytes.Buffer // never a TTY -> plain renderer
		err := ui.RunPipeline(context.Background(), "up", &out,
			func(_ context.Context, con *ui.Console) error {
				for _, pr := range refs {
					stepFetchSource(con, pr.Ref)
				}
				return nil
			})
		if err != nil {
			t.Fatal(err)
		}
		return out.String()
	}

	// Online: the network ref itself must appear, byte-for-byte.
	online := emit([]config.PackRef{{Ref: "oci://ghcr.io/cube-idp/packs/gitea:0.1.0"}})
	if want := "▸ [pack] fetching oci://ghcr.io/cube-idp/packs/gitea:0.1.0\n"; !strings.Contains(online, want) {
		t.Fatalf("online fetch-source line missing %q in:\n%s", want, online)
	}

	// Bundle: refs resolved via resolveBundleRefs print the bundle-local dir
	// and no oci:// ref survives.
	lk := &lock.File{Packs: []lock.Entry{{Ref: "oci://ghcr.io/x/packs/gitea:0.1.0", Name: "gitea"}}}
	resolved, err := resolveBundleRefs(
		[]config.PackRef{{Ref: "oci://ghcr.io/x/packs/gitea:0.1.0"}}, lk,
		func(name string) (string, bool) { return "/tmp/cube-idp-bundle-123/packs/" + name, true })
	if err != nil {
		t.Fatal(err)
	}
	offline := emit(resolved)
	if want := "▸ [pack] fetching /tmp/cube-idp-bundle-123/packs/gitea\n"; !strings.Contains(offline, want) {
		t.Fatalf("bundle fetch-source line missing %q in:\n%s", want, offline)
	}
	if strings.Contains(offline, "oci://") {
		t.Fatalf("bundle fetch-source output leaked an oci:// ref:\n%s", offline)
	}
}

// stubUnhealthyEngine implements engine.Engine with one permanently
// not-ready component — only Health matters to waitHealthy; every other
// method is a no-op present to satisfy the interface.
type stubUnhealthyEngine struct{}

func (stubUnhealthyEngine) Install(context.Context, *apply.Applier, time.Duration) error { return nil }
func (stubUnhealthyEngine) InstallManifests() ([]*unstructured.Unstructured, error) {
	return nil, nil
}
func (stubUnhealthyEngine) Deliver(context.Context, *pack.Rendered, engine.ArtifactRef) ([]*unstructured.Unstructured, error) {
	return nil, nil
}
func (stubUnhealthyEngine) DeliverGit(context.Context, string, engine.GitSource, []string) ([]*unstructured.Unstructured, error) {
	return nil, nil
}
func (stubUnhealthyEngine) DeliverSelf(context.Context, engine.ArtifactRef) ([]*unstructured.Unstructured, error) {
	return nil, nil
}
func (stubUnhealthyEngine) Poke(context.Context, *apply.Applier, string) error { return nil }
func (stubUnhealthyEngine) Health(context.Context, *apply.Applier) ([]engine.ComponentHealth, error) {
	return []engine.ComponentHealth{{Name: "kustomize-controller", Ready: false, Message: "reconciling"}}, nil
}
func (stubUnhealthyEngine) Uninstall(context.Context, *apply.Applier, time.Duration) error {
	return nil
}
func (stubUnhealthyEngine) OrdersDeliveries() bool { return true }

// TestWaitHealthyNarratesUnhealthyWait pins U1's engine-wait narration:
// while components stay unhealthy, waitHealthy emits StepLog events with
// stage "health" — the stage of the Progress step open during the wait, so
// the live renderer's log tail actually shows the lines (U2 Step 0; U1
// shipped stage "engine", which never rendered live because the tail only
// follows the open step's stage) — naming the not-ready components, paced
// by healthLogEvery
// (shrunk here — the package has no fake clock). The wait itself still
// times out with CUBE-3004 as before; the narration is additive.
func TestWaitHealthyNarratesUnhealthyWait(t *testing.T) {
	oldPoll, oldEvery := healthPoll, healthLogEvery
	healthPoll, healthLogEvery = 5*time.Millisecond, 30*time.Millisecond
	defer func() { healthPoll, healthLogEvery = oldPoll, oldEvery }()

	ch := make(chan event.Event, 256)
	con := ui.NewConsole(ch)
	err := waitHealthy(context.Background(), stubUnhealthyEngine{}, nil, con, 200*time.Millisecond)
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != diag.CodeEngineHealthTimeout {
		t.Fatalf("stub never becomes healthy: want CUBE-3004 timeout, got %v", err)
	}
	close(ch)
	var logs []event.StepLog
	for ev := range ch {
		if sl, ok := ev.(event.StepLog); ok && sl.Stage == "health" {
			logs = append(logs, sl)
		}
	}
	if len(logs) == 0 {
		t.Fatal(`no StepLog{Stage:"health"} events emitted during an unhealthy wait`)
	}
	for _, sl := range logs {
		if !strings.Contains(sl.Line, "waiting on: ") || !strings.Contains(sl.Line, "kustomize-controller") {
			t.Fatalf("narration line malformed: %q", sl.Line)
		}
	}
}

// fakeInternalProvider satisfies cluster.Provider plus
// cluster.InternalKubeconfiger — the kind-arm seam for spokeServerURL
// without a real kind cluster.
type fakeInternalProvider struct{ kc []byte }

func (f fakeInternalProvider) Ensure(context.Context, string, config.ClusterSpec) (*kube.Conn, error) {
	return nil, nil
}
func (f fakeInternalProvider) Delete(context.Context, string) error         { return nil }
func (f fakeInternalProvider) Exists(context.Context, string) (bool, error) { return false, nil }
func (f fakeInternalProvider) Kubeconfig(context.Context, string) ([]byte, error) {
	return f.kc, nil
}
func (f fakeInternalProvider) Diagnose(context.Context, string) []diag.Finding { return nil }
func (f fakeInternalProvider) InternalKubeconfig(context.Context, string) ([]byte, error) {
	return f.kc, nil
}

// TestSpokeClusterName pins GT7's naming: kind spokes are
// <cube>-spoke-<name>; existing spokes are whatever the context points at
// (Ensure ignores the name, so the spoke's own name suffices).
func TestSpokeClusterName(t *testing.T) {
	cube := &config.Cube{}
	cube.Metadata.Name = "dev"
	if got := spokeClusterName(cube, config.SpokeSpec{Name: "staging",
		Cluster: config.ClusterSpec{Provider: "kind"}}); got != "dev-spoke-staging" {
		t.Fatalf("kind spoke name: %q", got)
	}
	if got := spokeClusterName(cube, config.SpokeSpec{Name: "prod-eu",
		Cluster: config.ClusterSpec{Provider: "existing", Context: "eks-prod-eu"}}); got != "prod-eu" {
		t.Fatalf("existing spoke name: %q", got)
	}
}

// TestSpokeClusterSpecDefaultsKubernetesVersion pins the spoke node-version
// inheritance: a kind spoke with no explicit kubernetesVersion takes the
// hub's pin, and the documented "v1.33.1" default when the hub (provider
// existing) has none — a bare kind spoke must never render the invalid
// image "kindest/node:".
func TestSpokeClusterSpecDefaultsKubernetesVersion(t *testing.T) {
	cube := &config.Cube{}
	cube.Spec.Cluster = config.ClusterSpec{Provider: "kind", KubernetesVersion: "v1.32.0"}
	sp := config.SpokeSpec{Name: "staging", Cluster: config.ClusterSpec{Provider: "kind"}}
	if got := spokeClusterSpec(cube, sp).KubernetesVersion; got != "v1.32.0" {
		t.Fatalf("spoke must inherit the hub pin, got %q", got)
	}
	cube.Spec.Cluster = config.ClusterSpec{Provider: "existing", Context: "eks"}
	if got := spokeClusterSpec(cube, sp).KubernetesVersion; got != "v1.33.1" {
		t.Fatalf("existing hub: spoke must take the documented default, got %q", got)
	}
	sp.Cluster.KubernetesVersion = "v1.31.0"
	if got := spokeClusterSpec(cube, sp).KubernetesVersion; got != "v1.31.0" {
		t.Fatalf("explicit spoke version must win, got %q", got)
	}
	ex := config.SpokeSpec{Name: "prod-eu", Cluster: config.ClusterSpec{Provider: "existing", Context: "eks-prod-eu"}}
	if got := spokeClusterSpec(cube, ex).KubernetesVersion; got != "" {
		t.Fatalf("existing spokes get no version injected (node-creation field), got %q", got)
	}
}

// TestSpokeServerURL covers both arms of the hub-reachable endpoint pick:
// existing → the connection's own server URL; kind → the internal
// kubeconfig's server (shared docker network), never the host-published
// 127.0.0.1 endpoint the hub's pods cannot reach.
func TestSpokeServerURL(t *testing.T) {
	sp := config.SpokeSpec{Name: "prod-eu", Cluster: config.ClusterSpec{Provider: "existing", Context: "eks-prod-eu"}}
	conn := &kube.Conn{REST: &rest.Config{Host: "https://eks.example:443"}}
	prov, err := cluster.New(sp.Cluster, config.GatewaySpec{})
	if err != nil {
		t.Fatal(err)
	}
	got, err := spokeServerURL(context.Background(), prov, "prod-eu", sp, conn)
	if err != nil || got != "https://eks.example:443" {
		t.Fatalf("existing arm: got %q err %v", got, err)
	}

	kc, err := spoke.BuildKubeconfig("dev-spoke-staging", "https://dev-spoke-staging-control-plane:6443", []byte("CA"), "tok")
	if err != nil {
		t.Fatal(err)
	}
	kindSp := config.SpokeSpec{Name: "staging", Cluster: config.ClusterSpec{Provider: "kind"}}
	hostConn := &kube.Conn{REST: &rest.Config{Host: "https://127.0.0.1:52123"}}
	got, err = spokeServerURL(context.Background(), fakeInternalProvider{kc: kc}, "dev-spoke-staging", kindSp, hostConn)
	if err != nil || got != "https://dev-spoke-staging-control-plane:6443" {
		t.Fatalf("kind arm: got %q err %v", got, err)
	}
}

// ---- P7: per-pack delivery branch (delivery: repo) ----------------------

// fakePackEngine records which delivery shape was asked for. It satisfies
// packEngine (the narrow up-side seam), never the full engine.Engine.
type fakePackEngine struct {
	delivered      []string // pack names handed to Deliver (OCI shape)
	deliveredDeps  [][]string
	gitDelivered   []string // pack names handed to DeliverGit
	gitSources     []engine.GitSource
	gitDeliverDeps [][]string
	selfRefs       []engine.ArtifactRef // artifacts handed to DeliverSelf (P8)

	// wavegated backs OrdersDeliveries' negation — zero value (false) means
	// OrdersDeliveries() returns true (flux-like: no wave gate), so every
	// pre-DEP3 test's zero-value &fakePackEngine{} keeps its exact
	// behavior; p6 DEP3's wave-gate tests set wavegated true (argocd-like)
	// to exercise waitDepsHealthy.
	wavegated bool
	// health backs Health — the wave gate's collaborator; nil means "no
	// components reported" (never ready), matching allReady's own posture.
	health      []engine.ComponentHealth
	healthErr   error
	healthCalls int
}

func (f *fakePackEngine) Deliver(_ context.Context, r *pack.Rendered, _ engine.ArtifactRef) ([]*unstructured.Unstructured, error) {
	f.delivered = append(f.delivered, r.Name)
	f.deliveredDeps = append(f.deliveredDeps, r.DependsOn)
	o := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1", "kind": "ConfigMap",
		"metadata": map[string]any{"name": "oci-" + r.Name, "namespace": "d"}}}
	return []*unstructured.Unstructured{o}, nil
}

func (f *fakePackEngine) DeliverGit(_ context.Context, name string, src engine.GitSource, dependsOn []string) ([]*unstructured.Unstructured, error) {
	f.gitDelivered = append(f.gitDelivered, name)
	f.gitSources = append(f.gitSources, src)
	f.gitDeliverDeps = append(f.gitDeliverDeps, dependsOn)
	o := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1", "kind": "ConfigMap",
		"metadata": map[string]any{"name": "git-" + name, "namespace": "d"}}}
	return []*unstructured.Unstructured{o}, nil
}

func (f *fakePackEngine) DeliverSelf(_ context.Context, src engine.ArtifactRef) ([]*unstructured.Unstructured, error) {
	f.selfRefs = append(f.selfRefs, src)
	o := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1", "kind": "ConfigMap",
		"metadata": map[string]any{"name": "self-cube-engine", "namespace": "d"}}}
	return []*unstructured.Unstructured{o}, nil
}

// OrdersDeliveries defaults to true (flux-like — no wave gate); a test
// opts into the argocd-like false posture by setting wavegated true.
func (f *fakePackEngine) OrdersDeliveries() bool { return !f.wavegated }

func (f *fakePackEngine) Health(context.Context, *apply.Applier) ([]engine.ComponentHealth, error) {
	f.healthCalls++
	return f.health, f.healthErr
}

// fakePackApplier records applied/inventoried objects — the narrow
// packApplier seam (*apply.Applier satisfies it in production).
type fakePackApplier struct {
	applied  []string
	recorded []string
}

func names(objs []*unstructured.Unstructured) []string {
	var out []string
	for _, o := range objs {
		out = append(out, o.GetName())
	}
	return out
}

func (f *fakePackApplier) Apply(_ context.Context, objs []*unstructured.Unstructured, _ bool, _ time.Duration) error {
	f.applied = append(f.applied, names(objs)...)
	return nil
}

func (f *fakePackApplier) RecordInventory(_ context.Context, objs []*unstructured.Unstructured) error {
	f.recorded = append(f.recorded, names(objs)...)
	return nil
}

// fakeGiteaPacks records repo-side calls — the giteaPacks seam
// (*gitea.Client satisfies it in production).
type fakeGiteaPacks struct {
	ensured []string
	synced  map[string][]string // repo -> sorted synced paths
	branch  string
	msgs    []string
}

func (f *fakeGiteaPacks) EnsureRepo(_ context.Context, name string) (*gitea.Repo, error) {
	f.ensured = append(f.ensured, name)
	return &gitea.Repo{Owner: "gitea_admin", Name: name, DefaultBranch: "main",
		CloneURL: "http://gitea/gitea_admin/" + name + ".git"}, nil
}

func (f *fakeGiteaPacks) SyncDir(_ context.Context, owner, repo, branch, dir, message string, files map[string][]byte) (bool, error) {
	if f.synced == nil {
		f.synced = map[string][]string{}
	}
	var paths []string
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	f.synced[repo] = paths
	f.branch = branch
	f.msgs = append(f.msgs, message)
	return true, nil
}

func demoRendered(name string) *pack.Rendered {
	ns := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1", "kind": "Namespace",
		"metadata": map[string]any{"name": name}}}
	cm := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1", "kind": "ConfigMap",
		"metadata": map[string]any{"name": "seed", "namespace": name}}}
	return &pack.Rendered{Name: name, Version: "0.1.0", Objects: []*unstructured.Unstructured{ns, cm}}
}

// TestDeliverPackOCINeverTouchesGitea pins the P7 branch: a pack without
// delivery: repo takes the pre-P7 tail (push to zot + engine OCI source)
// and never opens a gitea session.
func TestDeliverPackOCINeverTouchesGitea(t *testing.T) {
	eng := &fakePackEngine{}
	ap := &fakePackApplier{}
	pushed := 0
	deps := deliverDeps{
		eng:     eng,
		applier: ap,
		pushOCI: func(_ context.Context, r *pack.Rendered, _ string) (engine.ArtifactRef, error) {
			pushed++
			return engine.ArtifactRef{Repo: "packs/" + r.Name, Tag: r.Version}, nil
		},
		gitea: func(context.Context) (giteaPacks, error) {
			t.Fatal("OCI delivery must never touch gitea")
			return nil, nil
		},
	}
	if err := deliverPack(context.Background(), deps, config.PackRef{Ref: "x"}, demoRendered("demo")); err != nil {
		t.Fatal(err)
	}
	if pushed != 1 || !reflect.DeepEqual(eng.delivered, []string{"demo"}) || len(eng.gitDelivered) != 0 {
		t.Fatalf("OCI branch: pushed=%d delivered=%v git=%v", pushed, eng.delivered, eng.gitDelivered)
	}
	if !reflect.DeepEqual(ap.applied, []string{"oci-demo"}) || !reflect.DeepEqual(ap.recorded, []string{"oci-demo"}) {
		t.Fatalf("OCI branch apply/inventory: %v / %v", ap.applied, ap.recorded)
	}
}

// TestDeliverPackRepoNeverTouchesOCIPusher pins the other half: a
// delivery: repo pack renders into a Gitea repo (cube-pack-<name>) and an
// engine git source — the OCI pusher and the engine's OCI Deliver are
// never invoked, and DeliverGit's objects are applied + inventoried
// exactly like the OCI path's.
func TestDeliverPackRepoNeverTouchesOCIPusher(t *testing.T) {
	eng := &fakePackEngine{}
	ap := &fakePackApplier{}
	g := &fakeGiteaPacks{}
	deps := deliverDeps{
		eng:     eng,
		applier: ap,
		pushOCI: func(context.Context, *pack.Rendered, string) (engine.ArtifactRef, error) {
			t.Fatal("repo delivery must never touch the OCI pusher")
			return engine.ArtifactRef{}, nil
		},
		gitea: func(context.Context) (giteaPacks, error) { return g, nil },
	}
	if err := deliverPack(context.Background(), deps, config.PackRef{Ref: "x", Delivery: "repo"}, demoRendered("demo")); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(g.ensured, []string{"cube-pack-demo"}) {
		t.Fatalf("gitea repo not ensured: %v", g.ensured)
	}
	wantFiles := []string{"manifests/00-namespace-demo.yaml", "manifests/01-configmap-seed.yaml"}
	if !reflect.DeepEqual(g.synced["cube-pack-demo"], wantFiles) {
		t.Fatalf("synced files: %v, want %v", g.synced["cube-pack-demo"], wantFiles)
	}
	if g.branch != "main" {
		t.Fatalf("sync branch: %q", g.branch)
	}
	if len(eng.delivered) != 0 || !reflect.DeepEqual(eng.gitDelivered, []string{"demo"}) {
		t.Fatalf("repo branch must DeliverGit only: oci=%v git=%v", eng.delivered, eng.gitDelivered)
	}
	src := eng.gitSources[0]
	if src.URL != "http://gitea-http.gitea.svc.cluster.local:3000/gitea_admin/cube-pack-demo.git" ||
		src.Branch != "main" || src.Path != "./" {
		t.Fatalf("git source: %+v", src)
	}
	if !reflect.DeepEqual(ap.applied, []string{"git-demo"}) || !reflect.DeepEqual(ap.recorded, []string{"git-demo"}) {
		t.Fatalf("repo branch apply/inventory: %v / %v", ap.applied, ap.recorded)
	}
}

// TestDeliverOrderRespectsDependsOn pins p6 DEP2's pass-3 contract:
// resolveAndDeliverPacks delivers in pack.ResolveOrder's resolved order, not
// declared order. Declared order here is gateway, b (dependsOn a), a — the
// graph must still deliver gateway first (always), then a before b.
func TestDeliverOrderRespectsDependsOn(t *testing.T) {
	eng := &fakePackEngine{}
	ap := &fakePackApplier{}
	deps := deliverDeps{
		eng:     eng,
		applier: ap,
		pushOCI: func(_ context.Context, r *pack.Rendered, _ string) (engine.ArtifactRef, error) {
			return engine.ArtifactRef{Repo: "packs/" + r.Name, Tag: r.Version}, nil
		},
		gitea: func(context.Context) (giteaPacks, error) {
			t.Fatal("no repo-delivered pack in this cube")
			return nil, nil
		},
	}
	refs := []config.PackRef{{Ref: "gw"}, {Ref: "b"}, {Ref: "a"}}
	packs := []*pack.Pack{
		{Name: "gateway"},
		{Name: "b", DependsOn: []string{"a"}},
		{Name: "a"},
	}
	renders := []*pack.Rendered{demoRendered("gateway"), demoRendered("b"), demoRendered("a")}

	con := ui.NewConsole(make(chan event.Event, 256))
	// eng.OrdersDeliveries() defaults true (flux-like) — the wave gate
	// never runs, so a nil *apply.Applier is never dereferenced.
	if _, err := resolveAndDeliverPacks(context.Background(), con, deps, nil, refs, packs, renders); err != nil {
		t.Fatalf("resolveAndDeliverPacks: %v", err)
	}
	want := []string{"gateway", "a", "b"}
	if !reflect.DeepEqual(eng.delivered, want) {
		t.Fatalf("delivery order = %v, want %v (gateway, a, b)", eng.delivered, want)
	}
	wantDeps := [][]string{nil, nil, {"a"}}
	if !reflect.DeepEqual(eng.deliveredDeps, wantDeps) {
		t.Fatalf("Rendered.DependsOn threaded into Deliver = %v, want %v (p6 DEP3)", eng.deliveredDeps, wantDeps)
	}
}

// TestWaitDepsHealthyTimesOutAsCUBE3011 pins p6 DEP3's argocd-side wave-gate
// contract directly: a dependency that never reports Ready times out as
// CUBE-3011 (bounded by the caller's timeout, no infinite spinner).
func TestWaitDepsHealthyTimesOutAsCUBE3011(t *testing.T) {
	eng := &fakePackEngine{health: []engine.ComponentHealth{{Name: "cube-idp-a", Ready: false}}}
	err := waitDepsHealthy(context.Background(), eng, nil, "b", []string{"a"}, 20*time.Millisecond, 5*time.Millisecond)
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != diag.CodeEngineDepWait {
		t.Fatalf("want CUBE-3011, got %v", err)
	}
	if eng.healthCalls == 0 {
		t.Fatal("waitDepsHealthy must poll Health at least once before timing out")
	}
}

// TestWaitDepsHealthyReturnsOnceReady pins the success path: once every
// dependency's component (cube-idp-<dep>) reports Ready, waitDepsHealthy
// returns nil without waiting for the full timeout.
func TestWaitDepsHealthyReturnsOnceReady(t *testing.T) {
	eng := &fakePackEngine{health: []engine.ComponentHealth{{Name: "cube-idp-a", Ready: true}}}
	if err := waitDepsHealthy(context.Background(), eng, nil, "b", []string{"a"}, time.Minute, time.Millisecond); err != nil {
		t.Fatalf("waitDepsHealthy: %v", err)
	}
}

// TestWaitDepsHealthyNoDepsIsNoop pins the zero-deps fast path: a
// dependency-free pack never calls Health at all.
func TestWaitDepsHealthyNoDepsIsNoop(t *testing.T) {
	eng := &fakePackEngine{}
	if err := waitDepsHealthy(context.Background(), eng, nil, "solo", nil, time.Minute, time.Millisecond); err != nil {
		t.Fatalf("waitDepsHealthy: %v", err)
	}
	if eng.healthCalls != 0 {
		t.Fatalf("a pack with no deps must never call Health, got %d calls", eng.healthCalls)
	}
}

// TestWaveGateSkippedWhenEngineOrdersDeliveries pins p6 DEP3's flux-side
// integration contract: an engine that answers OrdersDeliveries true (flux)
// must see ZERO Health calls from resolveAndDeliverPacks's wave gate, even
// for a pack with unresolved DependsOn — flux's Kustomization
// spec.dependsOn does the ordering in-cluster, so `up` never polls Health
// itself for this engine.
func TestWaveGateSkippedWhenEngineOrdersDeliveries(t *testing.T) {
	eng := &fakePackEngine{} // wavegated=false → OrdersDeliveries() true
	ap := &fakePackApplier{}
	deps := deliverDeps{
		eng:     eng,
		applier: ap,
		pushOCI: func(_ context.Context, r *pack.Rendered, _ string) (engine.ArtifactRef, error) {
			return engine.ArtifactRef{Repo: "packs/" + r.Name, Tag: r.Version}, nil
		},
		gitea: func(context.Context) (giteaPacks, error) {
			t.Fatal("no repo-delivered pack in this cube")
			return nil, nil
		},
	}
	refs := []config.PackRef{{Ref: "gw"}, {Ref: "a"}, {Ref: "b"}}
	packs := []*pack.Pack{
		{Name: "gateway"},
		{Name: "a"},
		{Name: "b", DependsOn: []string{"a"}},
	}
	renders := []*pack.Rendered{demoRendered("gateway"), demoRendered("a"), demoRendered("b")}

	con := ui.NewConsole(make(chan event.Event, 256))
	if _, err := resolveAndDeliverPacks(context.Background(), con, deps, nil, refs, packs, renders); err != nil {
		t.Fatalf("resolveAndDeliverPacks: %v", err)
	}
	if eng.healthCalls != 0 {
		t.Fatalf("OrdersDeliveries-true engine must see zero wave-gate Health calls, got %d", eng.healthCalls)
	}
}

// TestWaveGateBlocksDeliveryUntilDepHealthy pins p6 DEP3's argocd-side
// integration contract end-to-end: an engine that answers OrdersDeliveries
// false (argocd) must have resolveAndDeliverPacks poll Health via the wave
// gate before delivering a dependent pack; here the dependency is already
// Ready, so delivery proceeds and both packs land.
func TestWaveGateBlocksDeliveryUntilDepHealthy(t *testing.T) {
	eng := &fakePackEngine{
		wavegated: true, // argocd-like: no native ordering
		health:    []engine.ComponentHealth{{Name: "cube-idp-a", Ready: true}},
	}
	ap := &fakePackApplier{}
	deps := deliverDeps{
		eng:     eng,
		applier: ap,
		pushOCI: func(_ context.Context, r *pack.Rendered, _ string) (engine.ArtifactRef, error) {
			return engine.ArtifactRef{Repo: "packs/" + r.Name, Tag: r.Version}, nil
		},
		gitea: func(context.Context) (giteaPacks, error) {
			t.Fatal("no repo-delivered pack in this cube")
			return nil, nil
		},
	}
	refs := []config.PackRef{{Ref: "gw"}, {Ref: "a"}, {Ref: "b"}}
	packs := []*pack.Pack{
		{Name: "gateway"},
		{Name: "a"},
		{Name: "b", DependsOn: []string{"a"}},
	}
	renders := []*pack.Rendered{demoRendered("gateway"), demoRendered("a"), demoRendered("b")}

	con := ui.NewConsole(make(chan event.Event, 256))
	if _, err := resolveAndDeliverPacks(context.Background(), con, deps, nil, refs, packs, renders); err != nil {
		t.Fatalf("resolveAndDeliverPacks: %v", err)
	}
	if !reflect.DeepEqual(eng.delivered, []string{"gateway", "a", "b"}) {
		t.Fatalf("delivery order = %v, want [gateway a b]", eng.delivered)
	}
	if eng.healthCalls == 0 {
		t.Fatal("OrdersDeliveries-false engine must have the wave gate poll Health at least once")
	}
}

// TestUpFailsFastOnDepCycle pins the fail-fast half of p6 DEP2 (spec §3.3):
// a dependency cycle must be caught by the graph pass BEFORE the deliver
// pass runs at all — resolveAndDeliverPacks must return CUBE-4019 with ZERO
// deliverPack invocations recorded, not fail mid-delivery after some packs
// already reached the cluster.
func TestUpFailsFastOnDepCycle(t *testing.T) {
	eng := &fakePackEngine{}
	ap := &fakePackApplier{}
	deps := deliverDeps{
		eng:     eng,
		applier: ap,
		pushOCI: func(_ context.Context, r *pack.Rendered, _ string) (engine.ArtifactRef, error) {
			return engine.ArtifactRef{Repo: "packs/" + r.Name, Tag: r.Version}, nil
		},
		gitea: func(context.Context) (giteaPacks, error) {
			t.Fatal("no repo-delivered pack in this cube")
			return nil, nil
		},
	}
	refs := []config.PackRef{{Ref: "gw"}, {Ref: "a"}, {Ref: "b"}}
	packs := []*pack.Pack{
		{Name: "gateway"},
		{Name: "a", DependsOn: []string{"b"}},
		{Name: "b", DependsOn: []string{"a"}},
	}
	renders := []*pack.Rendered{demoRendered("gateway"), demoRendered("a"), demoRendered("b")}

	con := ui.NewConsole(make(chan event.Event, 256))
	_, err := resolveAndDeliverPacks(context.Background(), con, deps, nil, refs, packs, renders)
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != diag.CodePackDepCycle {
		t.Fatalf("want CUBE-4019, got %v", err)
	}
	if len(eng.delivered) != 0 || len(eng.gitDelivered) != 0 {
		t.Fatalf("a cycle must abort BEFORE any delivery: oci=%v git=%v", eng.delivered, eng.gitDelivered)
	}
	if len(ap.applied) != 0 || len(ap.recorded) != 0 {
		t.Fatalf("a cycle must abort before any apply/inventory: applied=%v recorded=%v", ap.applied, ap.recorded)
	}
}

// TestRenderedFilesStableNaming pins the repo file layout: order-indexed
// manifests/NN-<kind>-<name>.yaml — stable names give stable git diffs.
func TestRenderedFilesStableNaming(t *testing.T) {
	files, err := renderedFiles(demoRendered("demo"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("want 2 files, got %v", files)
	}
	ns, ok := files["manifests/00-namespace-demo.yaml"]
	if !ok || !strings.Contains(string(ns), "kind: Namespace") {
		t.Fatalf("namespace file wrong: %v / %s", ok, ns)
	}
	cm, ok := files["manifests/01-configmap-seed.yaml"]
	if !ok || !strings.Contains(string(cm), "kind: ConfigMap") {
		t.Fatalf("configmap file wrong: %v / %s", ok, cm)
	}
}

// TestOrderPackRefsPrependsGatewayOnly pins orderPackRefs' shrunk p6 DEP2
// contract: it only prepends the gateway ref. The gitea-hoist guarantee
// (decision 13) that this function used to implement moved to
// pack.ResolveOrder's implicit repo->gitea edge — covered by that package's
// TestResolveOrderImplicitRepoEdge / TestResolveOrderRepoDeliveryGuaranteeAgainstArgocd
// (case 9) — so orderPackRefs itself no longer branches on delivery: repo at
// all; declared order (gitea included) passes through untouched.
func TestOrderPackRefsPrependsGatewayOnly(t *testing.T) {
	gw := "oci://ghcr.io/cube-idp/packs/traefik:0.2.0"
	a := config.PackRef{Ref: "oci://ghcr.io/cube-idp/packs/argocd:0.2.0"}
	b := config.PackRef{Ref: "oci://ghcr.io/cube-idp/packs/backstage:0.2.0", Delivery: "repo"}
	g := config.PackRef{Ref: "oci://ghcr.io/cube-idp/packs/gitea:0.2.0"}

	got := orderPackRefs(gw, []config.PackRef{a, b, g})
	want := []string{gw, a.Ref, b.Ref, g.Ref}
	var gotRefs []string
	for _, r := range got {
		gotRefs = append(gotRefs, r.Ref)
	}
	if !reflect.DeepEqual(gotRefs, want) {
		t.Fatalf("orderPackRefs must only prepend the gateway ref, declared order otherwise untouched:\n got %v\nwant %v", gotRefs, want)
	}

	// No repo-delivered pack: same gateway-prepend-only behavior.
	got = orderPackRefs(gw, []config.PackRef{a, {Ref: g.Ref}})
	gotRefs = nil
	for _, r := range got {
		gotRefs = append(gotRefs, r.Ref)
	}
	if !reflect.DeepEqual(gotRefs, []string{gw, a.Ref, g.Ref}) {
		t.Fatalf("gateway-prepend-only: %v", gotRefs)
	}
}

// TestGiteaSessionGate pins the readiness gate: delivery is asynchronous
// (delivered != ready), so the session builder polls until the attempt
// succeeds and types the terminal timeout as CUBE-7301.
func TestGiteaSessionGate(t *testing.T) {
	calls := 0
	g := &fakeGiteaPacks{}
	cli, stop, err := giteaSession(context.Background(), 2*time.Second, time.Millisecond,
		func(context.Context) (giteaPacks, func(), error) {
			calls++
			if calls < 3 {
				return nil, nil, errors.New("gitea not up yet")
			}
			return g, func() {}, nil
		})
	if err != nil || cli != g {
		t.Fatalf("gate must succeed once the attempt does: %v", err)
	}
	stop()
	if calls != 3 {
		t.Fatalf("gate must poll: %d calls", calls)
	}

	_, _, err = giteaSession(context.Background(), 5*time.Millisecond, time.Millisecond,
		func(context.Context) (giteaPacks, func(), error) {
			return nil, nil, errors.New("still down")
		})
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != diag.CodeRepoGiteaUnavailable {
		t.Fatalf("gate timeout must be CUBE-7301, got: %v", err)
	}
}

// ---- P8: engine self-management (engine.selfManage, GT16) ----------------

// fakeHealthEngine is a full engine.Engine stub whose Health is canned and
// counted — all the P8 preflight (installNeedsSSA/engineHealthyAtStart)
// consumes. The applier is never touched and may be nil.
type fakeHealthEngine struct {
	health []engine.ComponentHealth
	err    error
	calls  int
}

func (f *fakeHealthEngine) Install(context.Context, *apply.Applier, time.Duration) error { return nil }
func (f *fakeHealthEngine) InstallManifests() ([]*unstructured.Unstructured, error) {
	return nil, nil
}
func (f *fakeHealthEngine) Deliver(context.Context, *pack.Rendered, engine.ArtifactRef) ([]*unstructured.Unstructured, error) {
	return nil, nil
}
func (f *fakeHealthEngine) DeliverGit(context.Context, string, engine.GitSource, []string) ([]*unstructured.Unstructured, error) {
	return nil, nil
}
func (f *fakeHealthEngine) DeliverSelf(context.Context, engine.ArtifactRef) ([]*unstructured.Unstructured, error) {
	return nil, nil
}
func (f *fakeHealthEngine) Poke(context.Context, *apply.Applier, string) error { return nil }
func (f *fakeHealthEngine) Health(context.Context, *apply.Applier) ([]engine.ComponentHealth, error) {
	f.calls++
	return f.health, f.err
}
func (f *fakeHealthEngine) Uninstall(context.Context, *apply.Applier, time.Duration) error {
	return nil
}
func (f *fakeHealthEngine) OrdersDeliveries() bool { return true }

// TestSelfManageSSADecision pins the GT16 SSA rules on installNeedsSSA:
// selfManage off → always SSA, without even consulting Health (the pre-P8
// path stays byte-identical); selfManage on → SSA on first install (zero
// components), on unhealthy components (rule 3), and on a Health error —
// only a fully healthy engine skips SSA (rule 2, single owner).
func TestSelfManageSSADecision(t *testing.T) {
	ready := []engine.ComponentHealth{{Name: "a", Ready: true}, {Name: "b", Ready: true}}
	unready := []engine.ComponentHealth{{Name: "a", Ready: true}, {Name: "b", Ready: false, Message: "nope"}}

	off := &fakeHealthEngine{health: ready}
	if !installNeedsSSA(context.Background(), off, nil, false) {
		t.Fatal("selfManage off must always SSA")
	}
	if off.calls != 0 {
		t.Fatalf("selfManage off must not consult Health (pre-P8 path), got %d calls", off.calls)
	}

	cases := []struct {
		name string
		eng  *fakeHealthEngine
		want bool // want SSA
	}{
		{"first_install_zero_components", &fakeHealthEngine{}, true},
		{"unhealthy_components", &fakeHealthEngine{health: unready}, true},
		{"health_error", &fakeHealthEngine{err: errors.New("api down")}, true},
		{"healthy_engine_owns_itself", &fakeHealthEngine{health: ready}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := installNeedsSSA(context.Background(), tc.eng, nil, true)
			if got != tc.want {
				t.Fatalf("installNeedsSSA(selfManage=true) = %v, want %v", got, tc.want)
			}
			if tc.eng.calls != 1 {
				t.Fatalf("preflight must be exactly one Health call, got %d", tc.eng.calls)
			}
		})
	}
}

// TestSelfManageDeliverEngineSelf pins the GT16 rule-2 tail: the rendered
// install is pushed as the cube-engine artifact (fixed tag, the pushed
// Rendered carries the installObjs verbatim — tuning already applied by
// InstallManifests, so the artifact carries tuned bytes), the resulting
// artifact ref is handed to DeliverSelf, and the self-source objects are
// applied + inventoried. Gitea is never touched — GT16: zot only.
func TestSelfManageDeliverEngineSelf(t *testing.T) {
	eng := &fakePackEngine{}
	ap := &fakePackApplier{}
	var pushed []*pack.Rendered
	deps := deliverDeps{
		eng:        eng,
		applier:    ap,
		tunnelAddr: "127.0.0.1:5000",
		pushOCI: func(_ context.Context, r *pack.Rendered, addr string) (engine.ArtifactRef, error) {
			if addr != "127.0.0.1:5000" {
				t.Fatalf("push must use the registry tunnel, got %q", addr)
			}
			pushed = append(pushed, r)
			return engine.ArtifactRef{Repo: "packs/" + r.Name, Tag: r.Version, Digest: "sha256:d1"}, nil
		},
		gitea: func(context.Context) (giteaPacks, error) {
			t.Fatal("engine self-management must never touch gitea (GT16: zot only)")
			return nil, nil
		},
	}
	installObjs := []*unstructured.Unstructured{{Object: map[string]any{
		"apiVersion": "apps/v1", "kind": "Deployment",
		"metadata": map[string]any{"name": "source-controller", "namespace": "flux-system"},
		"spec":     map[string]any{"replicas": int64(2)}, // a tuned render — pushed verbatim
	}}}

	if err := deliverEngineSelf(context.Background(), deps, installObjs); err != nil {
		t.Fatal(err)
	}
	if len(pushed) != 1 || pushed[0].Name != engine.SelfArtifactName || pushed[0].Version != engineSelfTag {
		t.Fatalf("must push exactly the cube-engine:%s artifact, got %+v", engineSelfTag, pushed)
	}
	if !reflect.DeepEqual(pushed[0].Objects, installObjs) {
		t.Fatalf("the artifact must carry the rendered (tuned) install verbatim: %+v", pushed[0].Objects)
	}
	if len(eng.selfRefs) != 1 || eng.selfRefs[0].Repo != "packs/cube-engine" ||
		eng.selfRefs[0].Tag != engineSelfTag || eng.selfRefs[0].Digest != "sha256:d1" {
		t.Fatalf("DeliverSelf must receive the pushed artifact ref, got %+v", eng.selfRefs)
	}
	if !reflect.DeepEqual(ap.applied, []string{"self-cube-engine"}) || !reflect.DeepEqual(ap.recorded, []string{"self-cube-engine"}) {
		t.Fatalf("self-source objects must be applied AND inventoried: %v / %v", ap.applied, ap.recorded)
	}
	// No pack-shaped delivery may have happened.
	if len(eng.delivered) != 0 || len(eng.gitDelivered) != 0 {
		t.Fatalf("self delivery must not invoke Deliver/DeliverGit: %v %v", eng.delivered, eng.gitDelivered)
	}
}

// TestSelfManageDeliverEngineSelfFailureIsCube3010 pins the failure typing:
// every arm (push shown here) is CUBE-3010 with re-run remediation.
func TestSelfManageDeliverEngineSelfFailureIsCube3010(t *testing.T) {
	deps := deliverDeps{
		eng:     &fakePackEngine{},
		applier: &fakePackApplier{},
		pushOCI: func(context.Context, *pack.Rendered, string) (engine.ArtifactRef, error) {
			return engine.ArtifactRef{}, errors.New("zot gone")
		},
		gitea: func(context.Context) (giteaPacks, error) { return nil, nil },
	}
	err := deliverEngineSelf(context.Background(), deps, nil)
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != diag.CodeEngineSelfManage {
		t.Fatalf("self-manage failure must be CUBE-3010, got: %v", err)
	}
	if !strings.Contains(de.Remediation, "cube-idp up") {
		t.Fatalf("remediation must name the `cube-idp up` re-run, got: %q", de.Remediation)
	}
}

// TestEnginePackRecordRow pins the engine's kubectl-get-packs row
// (engine-as-pack §3.3.7): delivery "engine", CUSTOMIZED tracks
// engine.values, no dependsOn.
func TestEnginePackRecordRow(t *testing.T) {
	pk := &pack.Pack{Name: "cube-engine-flux", Version: "0.1.0"}
	obj := pack.PackObject(pk, config.GatewaySpec{}, true, true, "engine", nil)
	spec := obj.Object["spec"].(map[string]any)
	if spec["delivery"] != "engine" || spec["customized"] != "yes" || spec["ready"] != true {
		t.Fatalf("engine record spec: %+v", spec)
	}
	if _, has := spec["dependsOn"]; has {
		t.Fatalf("engine record must carry no dependsOn: %+v", spec)
	}
}

// TestBundleRailsCheckRejectsValuesRef pins amendment 2's offline-honesty
// rail (CUBE-7007): --bundle vendors pack refs and images, never remote
// values sources, so a bundled cube carrying valuesRef must fail before any
// cluster mutation rather than silently reach for the network.
func TestBundleRailsCheckRejectsValuesRef(t *testing.T) {
	cube := &config.Cube{}
	cube.Spec.Packs = []config.PackRef{{Ref: "packs/x", ValuesRef: "github.com/a/v//x@v1"}}
	err := bundleRailsCheck(cube)
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != diag.CodeBundleRemoteSource {
		t.Fatalf("err = %v, want %s", err, diag.CodeBundleRemoteSource)
	}
	cube.Spec.Packs[0].ValuesRef = ""
	if err := bundleRailsCheck(cube); err != nil {
		t.Fatalf("clean cube rejected: %v", err)
	}
}

// TestBundleRailsCheckRejectsRemoteOrigin is the same rail for the OTHER
// remote source remote -f introduced (Task 10 Step 2b): the cube.yaml itself
// came off the network, so it is no more vendored into the bundle than a
// valuesRef is. Same code (CUBE-7007), same fail-before-mutation posture.
func TestBundleRailsCheckRejectsRemoteOrigin(t *testing.T) {
	cube := &config.Cube{}
	cube.MarkRemoteOrigin("oci://example/cfg:1", "oci:sha256:abc")
	err := bundleRailsCheck(cube)
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != diag.CodeBundleRemoteSource {
		t.Fatalf("err = %v, want %s", err, diag.CodeBundleRemoteSource)
	}
	// The ref is named so the operator can see WHICH source is unvendored.
	if !strings.Contains(de.Error(), "oci://example/cfg:1") {
		t.Fatalf("error does not name the ref: %v", err)
	}
	// A local-origin cube with no remote sources still passes.
	if err := bundleRailsCheck(&config.Cube{}); err != nil {
		t.Fatalf("local cube rejected: %v", err)
	}
}
