package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/go-containerregistry/pkg/registry"
	"sigs.k8s.io/yaml"

	"github.com/cube-idp/cube-idp/internal/config"
	"github.com/cube-idp/cube-idp/internal/diag"
	"github.com/cube-idp/cube-idp/internal/pack"
	"github.com/cube-idp/cube-idp/internal/ui"
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

// gh doctrine: args → never prompt; non-TTY bare invocation → refuse with
// the flag twin named, never hang (spec WP6 + Decision 4).
func TestPackInstallWithArgsNeverPrompts(t *testing.T) {
	file := cubeYAMLFixture(t)
	ref := "oci://ghcr.io/cube-idp/packs/demo:0.1.0"

	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetIn(&bytes.Buffer{})
	root.SetArgs([]string{"pack", "install", ref})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("pack install %s: %v\noutput: %s", ref, err, out.String())
	}

	cube, err := config.Load(file)
	if err != nil {
		t.Fatalf("reloading cube.yaml after install: %v", err)
	}
	last := cube.Spec.Packs[len(cube.Spec.Packs)-1]
	if last.Ref != ref {
		t.Fatalf("expected %q appended to spec.packs, got packs: %+v", ref, cube.Spec.Packs)
	}
	got := out.String()
	if !strings.Contains(got, "▸ [pack] added "+ref) {
		t.Fatalf("expected the added step line, got:\n%s", got)
	}
	if !strings.Contains(got, "next: cube-idp up") {
		t.Fatalf("expected the next-step hint, got:\n%s", got)
	}
	if strings.Contains(got, "hint:") || strings.Contains(got, "?") {
		t.Fatalf("args path must never prompt or print the prompt-path hint, got:\n%s", got)
	}
}

func TestPackInstallBareNonTTYRefuses(t *testing.T) {
	cubeYAMLFixture(t)

	done := make(chan error, 1)
	go func() {
		root := NewRootCmd()
		var out bytes.Buffer
		root.SetOut(&out)
		root.SetErr(&out)
		root.SetIn(&bytes.Buffer{}) // non-TTY stdin: prompting is forbidden
		root.SetArgs([]string{"pack", "install"})
		done <- root.ExecuteContext(context.Background())
	}()

	select {
	case err := <-done:
		var de *diag.Error
		if !errors.As(err, &de) || de.Code != diag.CodeConfirmRequired {
			t.Fatalf("want the CUBE-0010 non-interactive refusal, got %v", err)
		}
		if !strings.Contains(de.Remediation, "cube-idp pack install oci://") {
			t.Fatalf("refusal must name the flag twin (pass refs as arguments), got: %q", de.Remediation)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("bare pack install on non-TTY must refuse immediately, not hang waiting for input")
	}
}

// packMenuSeams routes the TTY-only menu path through test stubs (down_test's
// seam pattern): prompting allowed, menu returns names, confirm returns ok.
func packMenuSeams(t *testing.T, names []string, ok bool) {
	t.Helper()
	restoreAllowed, restoreMenu, restoreConfirm := packPromptsAllowed, packMenuSelect, packConfirm
	packPromptsAllowed = func(io.Reader, io.Writer) bool { return true }
	packMenuSelect = func(io.Reader, io.Writer) ([]string, error) { return names, nil }
	packConfirm = func(_ io.Reader, _ io.Writer, _ ui.ConfirmOpts) (bool, error) { return ok, nil }
	t.Cleanup(func() {
		packPromptsAllowed, packMenuSelect, packConfirm = restoreAllowed, restoreMenu, restoreConfirm
	})
}

// packlessCubeYAML writes a valid cube.yaml with an empty pack list (the
// init-generated default already contains every catalog pack, which would turn
// every menu selection into a duplicate no-op) and chdirs next to it.
func packlessCubeYAML(t *testing.T) string {
	t.Helper()
	t.Chdir(t.TempDir())
	cube := config.Default("packmenu-fixture")
	cube.Spec.Packs = nil
	raw, err := yaml.Marshal(cube)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("cube.yaml", raw, 0o644); err != nil {
		t.Fatal(err)
	}
	return "cube.yaml"
}

// Menu path (spec WP6): selection → one summary confirm → append → the
// scriptable-twin hint carrying the ACTUAL refs.
func TestPackInstallMenuAppendsAndHints(t *testing.T) {
	file := packlessCubeYAML(t)
	packMenuSeams(t, []string{"gitea"}, true)

	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetIn(&bytes.Buffer{})
	root.SetArgs([]string{"pack", "install"})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("menu path: %v\noutput: %s", err, out.String())
	}

	cube, err := config.Load(file)
	if err != nil {
		t.Fatalf("reloading cube.yaml: %v", err)
	}
	ref := "oci://ghcr.io/cube-idp/packs/gitea:0.1.0"
	if len(cube.Spec.Packs) != 1 || cube.Spec.Packs[0].Ref != ref {
		t.Fatalf("expected exactly the selected ref appended, got: %+v", cube.Spec.Packs)
	}
	got := stripANSI(out.String())
	if !strings.Contains(got, "▸ [pack] added "+ref) {
		t.Fatalf("expected the added step line, got:\n%s", got)
	}
	if !strings.Contains(got, "hint: cube-idp pack install "+ref) {
		t.Fatalf("expected the scriptable-twin hint with the actual ref, got:\n%s", got)
	}
}

// Decline path: the summary confirm answered No must change nothing and use
// the project's exact abort wording (TE-3.3's, from cmd/trust.go).
func TestPackInstallDeclineAborts(t *testing.T) {
	file := packlessCubeYAML(t)
	packMenuSeams(t, []string{"gitea"}, false)

	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetIn(&bytes.Buffer{})
	root.SetArgs([]string{"pack", "install"})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("decline must abort cleanly, got %v", err)
	}
	if !strings.Contains(out.String(), "aborted — nothing was changed") {
		t.Fatalf("want the exact abort wording, got:\n%s", out.String())
	}
	cube, err := config.Load(file)
	if err != nil {
		t.Fatal(err)
	}
	if len(cube.Spec.Packs) != 0 {
		t.Fatalf("decline must not mutate cube.yaml, got packs: %+v", cube.Spec.Packs)
	}
}

// Duplicate refs are skipped, never appended twice; an all-duplicate run
// leaves the file unchanged and says so.
func TestPackInstallDuplicateRefSkipped(t *testing.T) {
	file := cubeYAMLFixture(t) // default profile already carries gitea:0.1.0
	ref := "oci://ghcr.io/cube-idp/packs/gitea:0.1.0"

	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetIn(&bytes.Buffer{})
	root.SetArgs([]string{"pack", "install", ref})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("duplicate install must not fail: %v\noutput: %s", err, out.String())
	}
	got := out.String()
	if !strings.Contains(got, "skipped") || !strings.Contains(got, "cube.yaml unchanged") {
		t.Fatalf("expected skip + unchanged notices, got:\n%s", got)
	}
	cube, err := config.Load(file)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, p := range cube.Spec.Packs {
		if p.Ref == ref {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly one %s entry, got %d (packs: %+v)", ref, count, cube.Spec.Packs)
	}
}

// TestPackPushJSONStreamEmitsExpectedEventTypes is Step 5.3's JSON golden
// for pack push: --progress=json emits run_started/step_done/run_done, one
// event per line, on stdout.
func TestPackPushJSONStreamEmitsExpectedEventTypes(t *testing.T) {
	host := packLocalRegistry(t)
	dir := writeCmdDemoPack(t, "0.1.0")
	ref := "oci://" + host + "/packs/demo:0.1.0"

	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"pack", "push", dir, ref, "--progress=json"})
	if err := root.Execute(); err != nil {
		t.Fatalf("pack push --progress=json: %v\noutput: %s", err, out.String())
	}

	got := out.String()
	for _, want := range []string{
		`"type":"run_started","cmd":"pack","cube":""`,
		`"type":"step_done","stage":"pack","msg":"pushed ` + ref + `@sha256:`,
		`"type":"run_done","ok":true`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("JSON stream missing %q, got:\n%s", want, got)
		}
	}
	for _, line := range strings.Split(strings.TrimRight(got, "\n"), "\n") {
		var v any
		if err := json.Unmarshal([]byte(line), &v); err != nil {
			t.Fatalf("line is not valid JSON: %v\nline: %s", err, line)
		}
	}
}
