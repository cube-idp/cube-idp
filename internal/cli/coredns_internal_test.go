package cli

import (
	"context"
	"errors"
	"strings"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/cube-idp/cube-idp/internal/gateway"
)

// testCube is the cube whose block these rows splice.
const testCube = "dev"

// kubeadmCorefile is a kubeadm-shaped Corefile: the same structure the
// captured fixtures in internal/gateway/testdata carry, kept short here
// because the splice's own failure taxonomy is tested against those
// captures. What this file exercises is the read-modify-write around it.
const kubeadmCorefile = `.:53 {
    errors
    health {
       lameduck 5s
    }
    ready
    kubernetes cluster.local in-addr.arpa ip6.arpa {
       pods insecure
       fallthrough in-addr.arpa ip6.arpa
       ttl 30
    }
    prometheus :9153
    forward . /etc/resolv.conf
    cache 30
    loop
    reload
    loadbalance
}
`

// fakeConfigMaps is the hand-rolled stand-in for the namespaced dynamic
// resource: two function fields, one per method the splice uses, plus the
// call counts the retry assertions read.
type fakeConfigMaps struct {
	get     func(attempt int) (*unstructured.Unstructured, error)
	update  func(attempt int, obj *unstructured.Unstructured) error
	gets    int
	updates int
	written []string
}

func (f *fakeConfigMaps) Get(_ context.Context, name string, _ metav1.GetOptions, _ ...string) (*unstructured.Unstructured, error) {
	f.gets++
	if name != corednsName {
		return nil, apierrors.NewNotFound(schema.GroupResource{Resource: "configmaps"}, name)
	}
	return f.get(f.gets)
}

func (f *fakeConfigMaps) Update(_ context.Context, obj *unstructured.Unstructured, _ metav1.UpdateOptions, _ ...string) (*unstructured.Unstructured, error) {
	f.updates++
	corefile, _, _ := unstructured.NestedString(obj.Object, "data", corefileKey)
	f.written = append(f.written, corefile)
	if err := f.update(f.updates, obj); err != nil {
		return nil, err
	}
	return obj, nil
}

// corednsConfigMap builds the live object a Get answers with.
func corednsConfigMap(corefile string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1", "kind": "ConfigMap",
		"metadata": map[string]any{
			"name": corednsName, "namespace": corednsNamespace, "resourceVersion": "42",
		},
		"data": map[string]any{corefileKey: corefile},
	}}
}

// serving answers every Get with the same Corefile and accepts every Update.
func serving(corefile string) *fakeConfigMaps {
	return &fakeConfigMaps{
		get:    func(int) (*unstructured.Unstructured, error) { return corednsConfigMap(corefile), nil },
		update: func(int, *unstructured.Unstructured) error { return nil },
	}
}

// conflict is what the API server answers when the object moved on since the
// read.
func conflict() error {
	return apierrors.NewConflict(
		schema.GroupResource{Resource: "configmaps"}, corednsName, errors.New("object has been modified"))
}

// markers are this cube's block delimiters as they appear in a Corefile.
func markers() (string, string) {
	return "# cube-idp:begin " + testCube, "# cube-idp:end " + testCube
}

// TestSpliceCoreDNSInserts: a clean kubeadm Corefile gets the block, written
// back through one Update, with everything unmarked preserved.
func TestSpliceCoreDNSInserts(t *testing.T) {
	t.Parallel()
	cms := serving(kubeadmCorefile)

	if err := spliceCoreDNS(t.Context(), cms, testCube, testDomain); err != nil {
		t.Fatalf("spliceCoreDNS: %v", err)
	}
	if cms.gets != 1 || cms.updates != 1 {
		t.Errorf("gets = %d, updates = %d; want one of each", cms.gets, cms.updates)
	}
	begin, end := markers()
	written := cms.written[0]
	for _, want := range []string{begin, end, gateway.ServiceFQDN, "answer auto"} {
		if !strings.Contains(written, want) {
			t.Errorf("the written Corefile does not carry %q", want)
		}
	}
	if !strings.Contains(written, "forward . /etc/resolv.conf") {
		t.Error("unmarked content was lost; everything outside the markers is preserved")
	}
}

// TestSpliceCoreDNSReplaces: splicing a Corefile that already carries this
// cube's block replaces it in place — the same bytes come back out, which is
// what makes re-bootstrapping safe.
func TestSpliceCoreDNSReplaces(t *testing.T) {
	t.Parallel()
	spliced, err := gateway.CorefileSplice(kubeadmCorefile, testCube, testDomain)
	if err != nil {
		t.Fatalf("CorefileSplice: %v", err)
	}
	cms := serving(spliced)

	if err := spliceCoreDNS(t.Context(), cms, testCube, testDomain); err != nil {
		t.Fatalf("spliceCoreDNS: %v", err)
	}
	if cms.written[0] != spliced {
		t.Error("re-splicing an already spliced Corefile changed it")
	}
	begin, _ := markers()
	if n := strings.Count(cms.written[0], begin); n != 1 {
		t.Errorf("the written Corefile carries %d begin markers, want exactly 1", n)
	}
}

// TestSpliceCoreDNSRetriesOnConflict: a lost race is re-read and re-spliced,
// never re-sent. The second read serves a foreign edit, and the write that
// follows must carry it — which is what keeps a concurrent change intact.
func TestSpliceCoreDNSRetriesOnConflict(t *testing.T) {
	t.Parallel()
	const foreign = "# somebody else was here\n"
	cms := &fakeConfigMaps{
		get: func(attempt int) (*unstructured.Unstructured, error) {
			if attempt == 1 {
				return corednsConfigMap(kubeadmCorefile), nil
			}
			return corednsConfigMap(foreign + kubeadmCorefile), nil
		},
		update: func(attempt int, _ *unstructured.Unstructured) error {
			if attempt == 1 {
				return conflict()
			}
			return nil
		},
	}

	if err := spliceCoreDNS(t.Context(), cms, testCube, testDomain); err != nil {
		t.Fatalf("spliceCoreDNS: %v", err)
	}
	if cms.gets != 2 {
		t.Errorf("gets = %d; a conflict must re-read rather than re-send", cms.gets)
	}
	if !strings.Contains(cms.written[1], foreign) {
		t.Error("the retry dropped the concurrent edit it read")
	}
}

// TestSpliceCoreDNSExhaustsConflicts: retries are bounded, and what surfaces
// is still a conflict — asserted through the API sentinel, which unwraps.
func TestSpliceCoreDNSExhaustsConflicts(t *testing.T) {
	t.Parallel()
	cms := &fakeConfigMaps{
		get:    func(int) (*unstructured.Unstructured, error) { return corednsConfigMap(kubeadmCorefile), nil },
		update: func(int, *unstructured.Unstructured) error { return conflict() },
	}

	err := spliceCoreDNS(t.Context(), cms, testCube, testDomain)
	if err == nil {
		t.Fatal("endless conflicts reported success")
	}
	if !apierrors.IsConflict(err) {
		t.Errorf("err = %v, want a wrapped conflict", err)
	}
	if cms.updates != spliceAttempts {
		t.Errorf("updates = %d, want %d attempts", cms.updates, spliceAttempts)
	}
}

// TestSpliceCoreDNSStructuralFault: the live Corefile's structure is the
// pure splice's verdict, and CUBE-GWY-004 passes through the edge untouched
// — with nothing written, because there is nothing safe to write.
func TestSpliceCoreDNSStructuralFault(t *testing.T) {
	t.Parallel()
	begin, _ := markers()
	cms := serving(strings.Replace(kubeadmCorefile, "    errors", "    "+begin, 1))

	err := spliceCoreDNS(t.Context(), cms, testCube, testDomain)
	assertCode(t, err, gateway.CodeCorefileStructure)
	if cms.updates != 0 {
		t.Errorf("a structural fault still wrote %d times", cms.updates)
	}
}

// TestSpliceCoreDNSReadFailure: a failed read fails the bootstrap, wrapped
// uncoded with the object it could not read, and is never retried as if it
// were a lost race.
func TestSpliceCoreDNSReadFailure(t *testing.T) {
	t.Parallel()
	cms := &fakeConfigMaps{
		get:    func(int) (*unstructured.Unstructured, error) { return nil, errCluster },
		update: func(int, *unstructured.Unstructured) error { return nil },
	}

	err := spliceCoreDNS(t.Context(), cms, testCube, testDomain)
	if !errors.Is(err, errCluster) {
		t.Fatalf("err = %v, want it to wrap the read failure", err)
	}
	if cms.gets != 1 || cms.updates != 0 {
		t.Errorf("gets = %d, updates = %d; a read failure is not a retryable race", cms.gets, cms.updates)
	}
}
