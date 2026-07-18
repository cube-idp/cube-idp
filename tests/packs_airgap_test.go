package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPackManifestsNoAlwaysPull is the starter-pack twin of the engine-side
// TestInstallManifestNoAlwaysPull (internal/engine/argocd): the air-gap guard
// for `up --bundle` (Task 7). A kubelet ignores an image node-loaded from a
// vendor bundle when its pod pins imagePullPolicy: Always, reaching instead
// for a registry the air-gapped host cannot see — ImagePullBackOff on a host
// with no egress. This sweeps EVERY packs/*/manifests/*.yaml so a future pack
// (or a re-vendor of an existing one, e.g. argo-cd's upstream install.yaml,
// which ships Always on most control-plane containers) cannot regress it.
// Pure filesystem scan — no network, runs under -short.
func TestPackManifestsNoAlwaysPull(t *testing.T) {
	manifests, err := filepath.Glob(filepath.Join(packsTree(t), "*", "manifests", "*.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(manifests) == 0 {
		t.Fatal("glob matched no pack manifests — test is scanning the wrong path")
	}
	for _, m := range manifests {
		raw, err := os.ReadFile(m)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(raw), "\n") {
			if strings.TrimSpace(line) == "imagePullPolicy: Always" {
				t.Errorf("%s:%d pins imagePullPolicy: Always — node-loaded bundle images would be ignored under `up --bundle`; flip to IfNotPresent (and keep the flip in the pack's documented re-vendoring recipe)", m, i+1)
			}
		}
	}
}
