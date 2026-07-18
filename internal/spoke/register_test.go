package spoke

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/tools/clientcmd"
)

func testCred() *Credential {
	return &Credential{Token: "tok-123", CAData: []byte("CADATA")}
}

func TestBuildKubeconfigShape(t *testing.T) {
	kc, err := BuildKubeconfig("dev-spoke-staging", "https://dev-spoke-staging-control-plane:6443", testCred().CAData, testCred().Token)
	if err != nil {
		t.Fatal(err)
	}
	// Round-trip through client-go itself: the kubeconfig must be loadable.
	if _, err := clientcmd.RESTConfigFromKubeConfig(kc); err != nil {
		t.Fatalf("kubeconfig does not load: %v", err)
	}
	s := string(kc)
	for _, want := range []string{"dev-spoke-staging-control-plane:6443", "token: tok-123", "certificate-authority-data:"} {
		if !strings.Contains(s, want) {
			t.Fatalf("kubeconfig missing %q:\n%s", want, s)
		}
	}
}

func TestHubSecretsArgocd(t *testing.T) {
	objs, err := HubSecrets("argocd", "staging", "https://x:6443", testCred())
	if err != nil || len(objs) != 1 {
		t.Fatalf("objs=%v err=%v", objs, err)
	}
	o := objs[0]
	if o.GetNamespace() != "argocd" || o.GetName() != "cube-idp-spoke-staging" {
		t.Fatalf("wrong target: %s/%s", o.GetNamespace(), o.GetName())
	}
	if o.GetLabels()["argocd.argoproj.io/secret-type"] != "cluster" {
		t.Fatalf("missing argocd cluster label: %v", o.GetLabels())
	}
	data, _, _ := unstructuredNestedStringMap(o, "stringData")
	if data["server"] != "https://x:6443" || !strings.Contains(data["config"], "bearerToken") {
		t.Fatalf("bad cluster secret payload: %v", data)
	}
}

func TestHubSecretsFlux(t *testing.T) {
	objs, err := HubSecrets("flux", "staging", "https://x:6443", testCred())
	if err != nil || len(objs) != 1 {
		t.Fatalf("objs=%v err=%v", objs, err)
	}
	o := objs[0]
	if o.GetNamespace() != "flux-system" || o.GetName() != "cube-idp-spoke-staging" {
		t.Fatalf("wrong target: %s/%s", o.GetNamespace(), o.GetName())
	}
	data, _, _ := unstructuredNestedStringMap(o, "stringData")
	if !strings.Contains(data["value"], "token: tok-123") {
		t.Fatalf("flux secret must embed kubeconfig under key value: %v", data)
	}
}

// unstructuredNestedStringMap is the plan's 5-line helper over
// unstructured.NestedStringMap for reading a Secret's stringData.
func unstructuredNestedStringMap(o *unstructured.Unstructured, fields ...string) (map[string]string, bool, error) {
	return unstructured.NestedStringMap(o.Object, fields...)
}
