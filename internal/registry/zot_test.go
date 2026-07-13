package registry

import "testing"

func TestManifestsParseAndTarget(t *testing.T) {
	objs, err := Manifests()
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]bool{}
	for _, o := range objs {
		kinds[o.GetKind()] = true
		if o.GetKind() != "Namespace" && o.GetNamespace() != "cube-idp-system" {
			t.Fatalf("%s/%s must live in cube-idp-system", o.GetKind(), o.GetName())
		}
	}
	for _, want := range []string{"Namespace", "Deployment", "Service"} {
		if !kinds[want] {
			t.Fatalf("missing %s in zot manifests", want)
		}
	}
}
