package argocd

import (
	"strings"
	"testing"
)

// TestInstallManifestNoAlwaysPull is the air-gap guard for `up --bundle`
// (Task 7): a kubelet ignores an image node-loaded from a vendor bundle when
// the pod pins imagePullPolicy: Always, reaching instead for a registry the
// air-gapped host cannot see. Upstream argo-cd ships Always on most
// control-plane containers; hack/gen-argocd-manifests.sh rewrites them to
// IfNotPresent on every regen (hack/inject-argocd-cmd-params.awk), and this
// test fails if a regeneration ever loses that deviation.
func TestInstallManifestNoAlwaysPull(t *testing.T) {
	if strings.Contains(string(installYAML), "imagePullPolicy: Always") {
		t.Fatal("embedded argo-cd install.yaml pins imagePullPolicy: Always — node-loaded bundle images would be ignored; re-run hack/gen-argocd-manifests.sh (the awk injector rewrites Always -> IfNotPresent)")
	}
	// Sanity: the manifest still pins pull policy explicitly (the flip did not
	// simply strip the field), so the IfNotPresent intent is present.
	if !strings.Contains(string(installYAML), "imagePullPolicy: IfNotPresent") {
		t.Fatal("embedded argo-cd install.yaml has no imagePullPolicy: IfNotPresent — expected the air-gap-friendly policy")
	}
}
