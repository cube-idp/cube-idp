package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/cube-idp/cube-idp/internal/diag"
	"github.com/cube-idp/cube-idp/internal/pack"
)

// writePublishPack writes a minimal contract-v1 pack (pack.cue with name,
// version, description + one manifest) under parent/<name> and returns its
// path. Unlike writeCmdDemoPack it carries the description the index
// requires (contract v1) and lives under a caller-owned parent so the index
// tests can compose a multi-pack packs/ tree.
func writePublishPack(t *testing.T, parent, name, version, desc string) string {
	t.Helper()
	dir := filepath.Join(parent, name)
	if err := os.MkdirAll(filepath.Join(dir, "manifests"), 0o755); err != nil {
		t.Fatal(err)
	}
	cue := "name:        \"" + name + "\"\nversion:     \"" + version + "\"\n"
	if desc != "" {
		cue += "description: \"" + desc + "\"\n"
	}
	if err := os.WriteFile(filepath.Join(dir, "pack.cue"), []byte(cue), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifests", "cm.yaml"),
		[]byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: "+name+"\n  namespace: default\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

var digestRe = regexp.MustCompile(`sha256:[a-f0-9]+`)

// TestPackPublishPrintsDigest: `pack publish` validates the dir as a pack,
// pushes it, and prints the manifest digest — the packs-repo CI
// (hack/publish-changed.sh) greps that digest into digests.env.
func TestPackPublishPrintsDigest(t *testing.T) {
	host := packLocalRegistry(t)
	dir := writePublishPack(t, t.TempDir(), "demo", "1.0.0", "demo pack")

	out := mustRunCLI(t, "pack", "publish", dir, "--ref", "oci://"+host+"/packs/demo:1.0.0")
	if !digestRe.MatchString(out) {
		t.Fatalf("publish output has no sha256: digest:\n%s", out)
	}
	p, err := pack.Fetch(context.Background(), "oci://"+host+"/packs/demo:1.0.0", t.TempDir())
	if err != nil {
		t.Fatalf("Fetch after publish: %v", err)
	}
	if p.Name != "demo" || p.Version != "1.0.0" {
		t.Fatalf("round-trip metadata: %+v", p)
	}
}

// TestPackPublishRejectsVersionTagMismatch pins the GT9 invariant the CI
// relies on: the publish tag must equal pack.cue's version — a mismatch is
// CUBE-4001 with a fix line, raised BEFORE any push.
func TestPackPublishRejectsVersionTagMismatch(t *testing.T) {
	host := packLocalRegistry(t)
	dir := writePublishPack(t, t.TempDir(), "demo", "1.0.0", "demo pack")

	out, err := runCLI(t, "pack", "publish", dir, "--ref", "oci://"+host+"/packs/demo:9.9.9")
	if err == nil || !strings.Contains(err.Error(), "CUBE-4001") {
		t.Fatalf("want CUBE-4001 tag/version mismatch, got err=%v\noutput: %s", err, out)
	}
}

// TestPackPublishIndexBuild: `pack index build` walks a packs/ tree and
// writes the schemaVersion-1 index artifact payload; digests come from
// repeatable --digest flags (the mode the publish CI uses — digests.env
// lines from `pack publish` output).
func TestPackPublishIndexBuild(t *testing.T) {
	packsDir := t.TempDir()
	writePublishPack(t, packsDir, "alpha", "0.1.0", "first demo")
	writePublishPack(t, packsDir, "beta", "0.2.0", "second demo")
	outPath := filepath.Join(t.TempDir(), "index.json")

	mustRunCLI(t, "pack", "index", "build", packsDir, "-o", outPath,
		"--ref-base", "oci://ghcr.io/cube-idp/packs",
		"--digest", "alpha=sha256:aaaa", "--digest", "beta=sha256:bbbb")

	raw, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	var idx struct {
		SchemaVersion int `json:"schemaVersion"`
		Packs         []struct {
			Name, Version, Description, Ref, Digest string
		} `json:"packs"`
	}
	if err := json.Unmarshal(raw, &idx); err != nil {
		t.Fatalf("index.json does not parse: %v\n%s", err, raw)
	}
	if idx.SchemaVersion != 1 || len(idx.Packs) != 2 {
		t.Fatalf("schema shape: %+v", idx)
	}
	// Deterministic order: sorted by name.
	a, b := idx.Packs[0], idx.Packs[1]
	if a.Name != "alpha" || a.Version != "0.1.0" || a.Description != "first demo" ||
		a.Ref != "oci://ghcr.io/cube-idp/packs/alpha:0.1.0" || a.Digest != "sha256:aaaa" {
		t.Fatalf("alpha entry: %+v", a)
	}
	if b.Name != "beta" || b.Version != "0.2.0" || b.Description != "second demo" ||
		b.Ref != "oci://ghcr.io/cube-idp/packs/beta:0.2.0" || b.Digest != "sha256:bbbb" {
		t.Fatalf("beta entry: %+v", b)
	}
}

// TestPackPublishIndexBuildRequiresDescription: contract v1 — a pack
// without a description fails the index build with a typed error, not a
// silently empty field.
func TestPackPublishIndexBuildRequiresDescription(t *testing.T) {
	packsDir := t.TempDir()
	writePublishPack(t, packsDir, "nodesc", "0.1.0", "")
	outPath := filepath.Join(t.TempDir(), "index.json")

	_, err := runCLI(t, "pack", "index", "build", packsDir, "-o", outPath,
		"--digest", "nodesc=sha256:cccc")
	if err == nil || !strings.Contains(err.Error(), "description") {
		t.Fatalf("want contract-v1 description error, got %v", err)
	}
}

// TestPackPublishIndexBuildNeedsDigestSource: with neither --digest nor
// --from-registry the build fails fast with a typed error whose fix line
// names both mechanisms (diag doctrine: the remediation carries the fix,
// Error() carries only code + summary).
func TestPackPublishIndexBuildNeedsDigestSource(t *testing.T) {
	packsDir := t.TempDir()
	writePublishPack(t, packsDir, "alpha", "0.1.0", "first demo")
	outPath := filepath.Join(t.TempDir(), "index.json")

	_, err := runCLI(t, "pack", "index", "build", packsDir, "-o", outPath)
	var de *diag.Error
	if err == nil || !errors.As(err, &de) {
		t.Fatalf("want typed diag error for missing digest source, got %v", err)
	}
	if !strings.Contains(de.Remediation, "--digest") || !strings.Contains(de.Remediation, "--from-registry") {
		t.Fatalf("fix line must name both digest mechanisms, got %q", de.Remediation)
	}
}

// TestPackPublishIndexBuildFromRegistry: --from-registry resolves each
// missing digest by HEADing the registry (never pulling) — the CI mode for
// packs not republished in the current run.
func TestPackPublishIndexBuildFromRegistry(t *testing.T) {
	host := packLocalRegistry(t)
	packsDir := t.TempDir()
	dir := writePublishPack(t, packsDir, "gamma", "0.3.0", "third demo")

	pub := mustRunCLI(t, "pack", "publish", dir, "--ref", "oci://"+host+"/packs/gamma:0.3.0")
	want := digestRe.FindString(pub)
	if want == "" {
		t.Fatalf("no digest in publish output:\n%s", pub)
	}

	outPath := filepath.Join(t.TempDir(), "index.json")
	mustRunCLI(t, "pack", "index", "build", packsDir, "-o", outPath,
		"--ref-base", "oci://"+host+"/packs", "--from-registry")

	raw, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	var idx struct {
		Packs []struct{ Name, Digest string } `json:"packs"`
	}
	if err := json.Unmarshal(raw, &idx); err != nil {
		t.Fatal(err)
	}
	if len(idx.Packs) != 1 || idx.Packs[0].Name != "gamma" || idx.Packs[0].Digest != want {
		t.Fatalf("from-registry digest: got %+v, want %s", idx.Packs, want)
	}
}

// TestPackPublishIndexPush: `pack index push` ships a built index.json as
// an OCI artifact and prints its digest; the pushed tag must resolve to
// exactly that digest.
func TestPackPublishIndexPush(t *testing.T) {
	host := packLocalRegistry(t)
	packsDir := t.TempDir()
	writePublishPack(t, packsDir, "alpha", "0.1.0", "first demo")
	idxPath := filepath.Join(t.TempDir(), "index.json")
	mustRunCLI(t, "pack", "index", "build", packsDir, "-o", idxPath,
		"--digest", "alpha=sha256:aaaa")

	ref := "oci://" + host + "/packs/index:latest"
	out := mustRunCLI(t, "pack", "index", "push", idxPath, "--ref", ref)
	digest := digestRe.FindString(out)
	if digest == "" {
		t.Fatalf("no digest in index push output:\n%s", out)
	}

	pin, err := pack.ResolveRemote(context.Background(), ref, t.TempDir())
	if err != nil {
		t.Fatalf("resolve after index push: %v", err)
	}
	if pin != "oci:"+digest {
		t.Fatalf("pushed tag resolves to %q, want oci:%s", pin, digest)
	}
}
