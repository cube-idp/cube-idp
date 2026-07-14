package cmd

import (
	"bytes"
	"context"
	"io"
	"log"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/registry"

	"github.com/rafpe/cube-idp/internal/pack"
)

// packLocalRegistry starts an in-process OCI registry (go-containerregistry's
// test registry — TEST-ONLY dependency) and returns its host:port. httptest
// serves plain HTTP on 127.0.0.1, matching PushPackDir's PlainHTTP gate.
func packLocalRegistry(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(registry.New(registry.Logger(log.New(io.Discard, "", 0))))
	t.Cleanup(srv.Close)
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	return u.Host
}

// writeCmdDemoPack writes a minimal valid pack directory (pack.cue + one
// manifest) and returns its path.
func writeCmdDemoPack(t *testing.T, version string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "pack.cue"),
		[]byte("name: \"demo\"\nversion: \""+version+"\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "manifests"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifests", "cm.yaml"),
		[]byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: demo\n  namespace: default\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func runPackPush(t *testing.T, args ...string) string {
	t.Helper()
	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(append([]string{"pack", "push"}, args...))
	if err := root.Execute(); err != nil {
		t.Fatalf("pack push %v: %v\noutput: %s", args, err, out.String())
	}
	return out.String()
}

// TestPackPushDefaultsTagToPackVersion: an untagged <oci-ref> gets ":<pack
// version from pack.cue>" appended before the push (brief: tag-defaulting is
// the CLI's job). Note the ref's host is 127.0.0.1:<port> — a colon in the
// HOST must not be mistaken for a tag separator.
func TestPackPushDefaultsTagToPackVersion(t *testing.T) {
	host := packLocalRegistry(t)
	dir := writeCmdDemoPack(t, "1.2.3")

	out := runPackPush(t, dir, "oci://"+host+"/packs/demo")

	if !strings.Contains(out, "/packs/demo:1.2.3@sha256:") {
		t.Fatalf("expected defaulted tag 1.2.3 and digest in output, got: %q", out)
	}
	p, err := pack.Fetch(context.Background(), "oci://"+host+"/packs/demo:1.2.3", t.TempDir())
	if err != nil {
		t.Fatalf("Fetch after push: %v", err)
	}
	if p.Name != "demo" || p.Version != "1.2.3" {
		t.Fatalf("round-trip metadata: %+v", p)
	}
}

// TestPackPushAlsoTagLatest: --also-tag latest applies a second tag to the
// SAME pushed manifest (one push, two tags — Owner Decisions #13).
func TestPackPushAlsoTagLatest(t *testing.T) {
	host := packLocalRegistry(t)
	dir := writeCmdDemoPack(t, "2.0.0")

	runPackPush(t, dir, "oci://"+host+"/packs/demo:2.0.0", "--also-tag", "latest")

	pVer, err := pack.Fetch(context.Background(), "oci://"+host+"/packs/demo:2.0.0", t.TempDir())
	if err != nil {
		t.Fatalf("Fetch by version tag: %v", err)
	}
	pLatest, err := pack.Fetch(context.Background(), "oci://"+host+"/packs/demo:latest", t.TempDir())
	if err != nil {
		t.Fatalf("Fetch by latest tag: %v", err)
	}
	if pVer.Pinned == "" || pVer.Pinned != pLatest.Pinned {
		t.Fatalf("tags point at different manifests: %q vs %q", pVer.Pinned, pLatest.Pinned)
	}
}

// TestPackPushPlainOutputByteStable pins the plain-mode output contract:
// exactly one ui.Step line, "▸ [pack] pushed <ref>@<digest>".
func TestPackPushPlainOutputByteStable(t *testing.T) {
	host := packLocalRegistry(t)
	dir := writeCmdDemoPack(t, "0.1.0")
	ref := "oci://" + host + "/packs/demo:0.1.0"

	out := runPackPush(t, dir, ref)

	if !strings.HasPrefix(out, "▸ [pack] pushed "+ref+"@sha256:") {
		t.Fatalf("plain output drifted: %q", out)
	}
	if strings.Count(out, "\n") != 1 {
		t.Fatalf("expected exactly one output line, got: %q", out)
	}
}
