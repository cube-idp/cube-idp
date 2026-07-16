package syncer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cube-idp/cube-idp/internal/apply"
	"github.com/cube-idp/cube-idp/internal/engine"
	"github.com/cube-idp/cube-idp/internal/pack"
)

// fakeEngine is a minimal engine.Engine test double: Deliver returns a
// single ConfigMap named cube-idp-<pack name> (standing in for a real
// engine's delivery objects) and Poke/Health/etc. are no-ops. It exists so
// SyncOnce's tests exercise the real Applier (envtest) and the real
// oci.PushRendered (against the in-memory registry below) without needing a
// full Flux/Argo CD install.
type fakeEngine struct{ poked []string }

func (f *fakeEngine) Install(context.Context, *apply.Applier, time.Duration) error { return nil }
func (f *fakeEngine) InstallManifests() ([]*unstructured.Unstructured, error)      { return nil, nil }
func (f *fakeEngine) Deliver(_ context.Context, r *pack.Rendered, _ engine.ArtifactRef) ([]*unstructured.Unstructured, error) {
	return []*unstructured.Unstructured{{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]any{
			"name":      "cube-idp-" + r.Name,
			"namespace": "default",
		},
	}}}, nil
}
func (f *fakeEngine) DeliverGit(context.Context, string, engine.GitSource) ([]*unstructured.Unstructured, error) {
	return nil, nil
}
func (f *fakeEngine) Poke(_ context.Context, _ *apply.Applier, packName string) error {
	f.poked = append(f.poked, packName)
	return nil
}
func (f *fakeEngine) Health(context.Context, *apply.Applier) ([]engine.ComponentHealth, error) {
	return nil, nil
}
func (f *fakeEngine) Uninstall(context.Context, *apply.Applier, time.Duration) error { return nil }

// newFakeOCIRegistry starts a minimal, in-process plain-HTTP OCI Distribution
// v2 registry sufficient for oras-go v2's remote.Repository client to push
// and tag a manifest (blob upload init+complete, manifest PUT/GET/PUT-by-tag)
// — exactly the sequence oci.PushRendered issues. It is deliberately
// narrow: no auth, no referrers API (our pushed manifest has no `subject`,
// so oras-go v2 never calls it), no blob GET (nothing in this flow reads a
// blob back). This lets SyncOnce's tests exercise the real, network-based
// oci.PushRendered — not just its in-memory test seam — without adding a
// registry-server dependency to go.mod.
func newFakeOCIRegistry(t *testing.T) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	manifests := map[string][]byte{}    // "<repo>|<ref>" (digest or tag) -> content
	manifestTypes := map[string]string{}
	var uploadSeq int64

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(path, "/blobs/uploads/"):
			mu.Lock()
			uploadSeq++
			id := uploadSeq
			mu.Unlock()
			w.Header().Set("Location", fmt.Sprintf("%supload-%d", path, id))
			w.WriteHeader(http.StatusAccepted)

		case r.Method == http.MethodPut && strings.Contains(path, "/blobs/uploads/"):
			// Blob content itself is never read back in this flow — discard
			// it (still fully drain the body so the client sees a clean 201).
			io.Copy(io.Discard, r.Body)
			digest := r.URL.Query().Get("digest")
			w.Header().Set("Docker-Content-Digest", digest)
			w.WriteHeader(http.StatusCreated)

		case r.Method == http.MethodPut && strings.Contains(path, "/manifests/"):
			idx := strings.Index(path, "/manifests/")
			repo := strings.TrimPrefix(path[:idx], "/v2/")
			ref := path[idx+len("/manifests/"):]
			data, _ := io.ReadAll(r.Body)
			sum := sha256.Sum256(data)
			digest := "sha256:" + hex.EncodeToString(sum[:])
			mt := r.Header.Get("Content-Type")
			mu.Lock()
			manifests[repo+"|"+digest] = data
			manifestTypes[repo+"|"+digest] = mt
			manifests[repo+"|"+ref] = data
			manifestTypes[repo+"|"+ref] = mt
			mu.Unlock()
			w.Header().Set("Docker-Content-Digest", digest)
			w.WriteHeader(http.StatusCreated)

		case r.Method == http.MethodGet && strings.Contains(path, "/manifests/"):
			idx := strings.Index(path, "/manifests/")
			repo := strings.TrimPrefix(path[:idx], "/v2/")
			ref := path[idx+len("/manifests/"):]
			mu.Lock()
			data, ok := manifests[repo+"|"+ref]
			mt := manifestTypes[repo+"|"+ref]
			mu.Unlock()
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			sum := sha256.Sum256(data)
			w.Header().Set("Content-Type", mt)
			w.Header().Set("Docker-Content-Digest", "sha256:"+hex.EncodeToString(sum[:]))
			w.Header().Set("Content-Length", strconv.Itoa(len(data)))
			w.WriteHeader(http.StatusOK)
			w.Write(data)

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestSyncOnceMergesInventoryWithPreexistingEntries is the Owner Decisions
// #14 regression: Applier.RecordInventory MERGES (object.ObjMetadataSet
// .Union with the loaded existing set), so a `sync` after an `up`-recorded
// entry must not orphan it. This drives SyncOnce end to end (real envtest
// Applier, real oci.PushRendered against an in-process registry, a fake
// Engine standing in for flux/argocd) and asserts both entries survive.
func TestSyncOnceMergesInventoryWithPreexistingEntries(t *testing.T) {
	if testREST == nil {
		t.Skip("KUBEBUILDER_ASSETS not set; envtest unavailable")
	}
	ctx := context.Background()
	a, err := apply.New(testREST, "synccube")
	if err != nil {
		t.Fatal(err)
	}

	// Simulate a pre-existing `up`-applied object already in the inventory.
	preexisting := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1", "kind": "ConfigMap",
		"metadata": map[string]any{"name": "from-up", "namespace": "default"},
	}}
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}}
	_ = a.Client().Create(ctx, ns) // envtest default namespace already exists in real clusters; ignore AlreadyExists-shaped races
	if err := a.Apply(ctx, []*unstructured.Unstructured{preexisting}, false, 30*time.Second); err != nil {
		t.Fatal(err)
	}
	if err := a.RecordInventory(ctx, []*unstructured.Unstructured{preexisting}); err != nil {
		t.Fatal(err)
	}
	before, err := a.LoadInventory(ctx)
	if err != nil || len(before) != 1 {
		t.Fatalf("preexisting inventory: %v (%d entries)", err, len(before))
	}

	// Bare manifest dir — exercises the D7 synthesis path end to end too.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "cm.yaml"),
		[]byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: synced\n  namespace: default\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	registry := newFakeOCIRegistry(t)
	addr := strings.TrimPrefix(registry.URL, "http://")

	fe := &fakeEngine{}
	deps := Deps{Applier: a, Engine: fe, Out: io.Discard, PushAddr: addr}
	result, err := SyncOnce(ctx, deps, dir)
	if err != nil {
		t.Fatalf("SyncOnce: %v", err)
	}
	if result.Pack != filepath.Base(dir) || result.Version != "0.0.0-dev" {
		t.Fatalf("Result identity: %+v", result)
	}
	if result.Digest == "" {
		t.Fatal("Result.Digest must be populated from the pushed artifact")
	}
	if len(fe.poked) != 1 || fe.poked[0] != result.Pack {
		t.Fatalf("Engine.Poke must be called once with the synced pack's name, got %v", fe.poked)
	}

	after, err := a.LoadInventory(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 2 {
		t.Fatalf("want 2 merged inventory entries (1 preexisting + 1 synced), got %d: %v", len(after), after)
	}

	deliveredName := "cube-idp-" + result.Pack
	got := &unstructured.Unstructured{}
	got.SetAPIVersion("v1")
	got.SetKind("ConfigMap")
	if err := a.Client().Get(ctx, client.ObjectKey{Namespace: "default", Name: "from-up"}, got); err != nil {
		t.Fatalf("preexisting object must survive sync's RecordInventory: %v", err)
	}
	got2 := &unstructured.Unstructured{}
	got2.SetAPIVersion("v1")
	got2.SetKind("ConfigMap")
	if err := a.Client().Get(ctx, client.ObjectKey{Namespace: "default", Name: deliveredName}, got2); err != nil {
		t.Fatalf("delivered object must be applied: %v", err)
	}
}
