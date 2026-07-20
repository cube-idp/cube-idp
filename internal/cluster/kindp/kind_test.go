package kindp

import (
	"errors"
	"strings"
	"testing"
	"time"

	"sigs.k8s.io/kind/pkg/cluster/nodes"
	kindexec "sigs.k8s.io/kind/pkg/exec"

	"github.com/cube-idp/cube-idp/internal/bundle"
	"github.com/cube-idp/cube-idp/internal/config"
	"github.com/cube-idp/cube-idp/internal/diag"
)

// TestLoadWithRetry_SucceedsOnSecondAttempt exercises the F10 retry seam: a
// load that fails once (the observed ~1-in-3 transient `ctr images import`
// failure) then succeeds must not surface an error to the caller.
func TestLoadWithRetry_SucceedsOnSecondAttempt(t *testing.T) {
	calls := 0
	load := func(n nodes.Node, path string) error {
		calls++
		if calls == 1 {
			return errors.New("transient ctr import failure")
		}
		return nil
	}
	if err := loadWithRetry(nil, "img.tar", load, 0); err != nil {
		t.Fatalf("loadWithRetry: got error %v, want nil after retry succeeds", err)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want exactly 2 (one retry)", calls)
	}
}

// TestLoadWithRetry_SucceedsFirstTry ensures the happy path does not pay for
// a retry it doesn't need.
func TestLoadWithRetry_SucceedsFirstTry(t *testing.T) {
	calls := 0
	load := func(n nodes.Node, path string) error {
		calls++
		return nil
	}
	if err := loadWithRetry(nil, "img.tar", load, time.Hour); err != nil {
		t.Fatalf("loadWithRetry: got error %v, want nil", err)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want exactly 1 (no retry needed)", calls)
	}
}

// TestLoadWithRetry_FailsAfterOneRetry_SurfacesStderr covers (b) and (c) of
// the transient-import retry: when both attempts fail, the resulting error
// must not swallow the
// underlying command's captured output (kind's exec.RunError.Error() omits
// its Output field), and must retry exactly once — not loop forever.
func TestLoadWithRetry_FailsAfterOneRetry_SurfacesStderr(t *testing.T) {
	calls := 0
	runErr := &kindexec.RunError{
		Command: []string{"ctr", "--namespace=k8s.io", "images", "import", "-"},
		Output:  []byte("ctr: content digest mismatch\n"),
		Inner:   errors.New("exit status 1"),
	}
	load := func(n nodes.Node, path string) error {
		calls++
		return runErr
	}
	err := loadWithRetry(nil, "img.tar", load, 0)
	if err == nil {
		t.Fatal("loadWithRetry: got nil error, want error after both attempts fail")
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want exactly 2 (initial + one retry, no more)", calls)
	}
	if !strings.Contains(err.Error(), "content digest mismatch") {
		t.Fatalf("error %q does not surface the underlying stderr", err.Error())
	}
	if !errors.Is(err, runErr) {
		t.Fatalf("error does not wrap the original RunError (errors.Is failed)")
	}
}

// TestLoadWithRetry_FailsAfterOneRetry_NonRunError confirms a plain error
// (not a kind exec.RunError) passes through unchanged rather than panicking
// or losing information.
func TestLoadWithRetry_FailsAfterOneRetry_NonRunError(t *testing.T) {
	plain := errors.New("cluster node unreachable")
	load := func(n nodes.Node, path string) error { return plain }
	err := loadWithRetry(nil, "img.tar", load, 0)
	if err == nil || !strings.Contains(err.Error(), "cluster node unreachable") {
		t.Fatalf("loadWithRetry: got %v, want it to contain the plain error message", err)
	}
}

// TestLoadArchiveIntoNodes_RetriesPerNode ensures the retry is applied
// per-node (per import call), not once for the whole batch: a load that
// fails on its first call for every node, but succeeds on the second call
// for every node, must succeed overall.
func TestLoadArchiveIntoNodes_RetriesPerNode(t *testing.T) {
	failedOnce := map[int]bool{}
	callIdx := -1
	load := func(n nodes.Node, path string) error {
		callIdx++
		nodeIdx := callIdx / 2 // two calls (attempt + retry) per node in the worst case
		if !failedOnce[nodeIdx] {
			failedOnce[nodeIdx] = true
			return errors.New("transient failure")
		}
		return nil
	}
	nodeList := make([]nodes.Node, 3)
	if err := loadArchiveIntoNodes(nodeList, "img.tar", load, 0); err != nil {
		t.Fatalf("loadArchiveIntoNodes: got error %v, want nil (each node recovers on retry)", err)
	}
	if len(failedOnce) != 3 {
		t.Fatalf("failedOnce has %d entries, want 3 (one first-failure per node)", len(failedOnce))
	}
}

// TestLoadArchiveIntoNodes_StopsAtFirstNodeThatExhaustsRetries confirms the
// loop aborts (rather than continuing to later nodes) once a node's import
// exhausts its retry.
func TestLoadArchiveIntoNodes_StopsAtFirstNodeThatExhaustsRetries(t *testing.T) {
	calls := 0
	load := func(n nodes.Node, path string) error {
		calls++
		return errors.New("permanent failure")
	}
	nodeList := make([]nodes.Node, 3)
	if err := loadArchiveIntoNodes(nodeList, "img.tar", load, 0); err == nil {
		t.Fatal("loadArchiveIntoNodes: got nil error, want error")
	}
	// First node: initial attempt + one retry = 2 calls, then loop returns.
	if calls != 2 {
		t.Fatalf("calls = %d, want exactly 2 (loop must stop at the first exhausted node)", calls)
	}
}

// TestLoadImagesIntoNodes_PermanentFailureSurfacesCUBE7006 asserts that a
// permanently-failing image load (both the initial attempt and its retry
// exhausted) surfaces the dedicated consume-side code CUBE-7006
// (CodeBundleImageLoadFail), not the vendor/produce-side CUBE-7002
// (CodeVendorPullFail) it used to be overloaded onto.
func TestLoadImagesIntoNodes_PermanentFailureSurfacesCUBE7006(t *testing.T) {
	load := func(n nodes.Node, path string) error {
		return errors.New("permanent containerd import failure")
	}
	nodeList := make([]nodes.Node, 1)
	imageTars := map[string]string{"example.com/app:v1": "app.tar"}

	err := loadImagesIntoNodes(nodeList, bundle.SortedImageLoads(imageTars), load, 0)
	if err == nil {
		t.Fatal("loadImagesIntoNodes: got nil error, want error")
	}
	var de *diag.Error
	if !errors.As(err, &de) {
		t.Fatalf("loadImagesIntoNodes: error %v is not a *diag.Error", err)
	}
	if de.Code != diag.CodeBundleImageLoadFail {
		t.Fatalf("loadImagesIntoNodes: code = %s, want %s (CUBE-7006)", de.Code, diag.CodeBundleImageLoadFail)
	}
}

// TestCertsDZeroGatewaySkipsInjection pins the S3 spoke contract's certs.d
// side: a Kind provider built with a zero GatewaySpec (spoke clusters have
// no gateway hostname and no zot) must request NO certs.d injection — the
// zero CertsD value — and must not touch the trust dir to decide that.
func TestCertsDZeroGatewaySkipsInjection(t *testing.T) {
	k := &Kind{gw: config.GatewaySpec{}}
	cd, err := k.certsD()
	if err != nil {
		t.Fatal(err)
	}
	if cd != (CertsD{}) {
		t.Fatalf("zero gateway must request no certs.d injection, got %+v", cd)
	}
}
