package pack

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/rafpe/cube-idp/internal/diag"
)

// makeGitFixture creates a local repo with a pack under packs/demo, tagged
// v0.1.0, using the git CLI (the same binary go-getter's GitGetter shells
// out to — if it is absent, these tests skip exactly like the getter would
// fail loudly in production).
func makeGitFixture(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git CLI not on PATH")
	}
	dir := t.TempDir()
	packDir := filepath.Join(dir, "packs", "demo")
	if err := os.MkdirAll(filepath.Join(packDir, "manifests"), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(packDir, "pack.cue"), []byte("name: \"demo\"\nversion: \"0.1.0\"\n"), 0o644)
	os.WriteFile(filepath.Join(packDir, "manifests", "cm.yaml"),
		[]byte("apiVersion: v1\nkind: ConfigMap\nmetadata: {name: demo, namespace: default}\n"), 0o644)
	for _, args := range [][]string{
		{"init", "-q"}, {"add", "."},
		{"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "init"},
		{"tag", "v0.1.0"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return dir
}

// TestSanitizeRefIsInjective covers (f): the old sanitizeRef mapped both
// '/' and '_' to the same output '_', so "a/b" and "a_b" collided on the
// same cache-dir segment. The fixed percent-encoding scheme must keep every
// distinct ref distinct.
func TestSanitizeRefIsInjective(t *testing.T) {
	pairs := [][2]string{
		{"a/b", "a_b"},
		{"a/b/c", "a_b_c"},
		{"pack%2Fname", "pack/name"}, // a literal '%' in the ref must not be mistaken for an escape
		{"a:b", "a_b"},
	}
	for _, p := range pairs {
		if sanitizeRef(p[0]) == sanitizeRef(p[1]) {
			t.Fatalf("sanitizeRef(%q) == sanitizeRef(%q) == %q — distinct refs collided", p[0], p[1], sanitizeRef(p[0]))
		}
	}
}

// TestGitCacheKeyIsInjective covers (f) for fetchGitTree's own key, which
// had the same class of bug: repoPath "org/a" + subdir "b/c" and repoPath
// "org/a" + subdir "b_c" both built the key "org_a@sha_b_c".
func TestGitCacheKeyIsInjective(t *testing.T) {
	a := gitCacheKey("org/a", "sha", "b/c")
	b := gitCacheKey("org/a", "sha", "b_c")
	if a == b {
		t.Fatalf("gitCacheKey collision: (repoPath=org/a, subdir=b/c) and (repoPath=org/a, subdir=b_c) both produced %q", a)
	}
	// A subdir-less ref must not collide with an equivalent single-segment
	// subdir either.
	c := gitCacheKey("org/a_b", "sha", "")
	d := gitCacheKey("org/a", "sha", "b")
	if c == d {
		t.Fatalf("gitCacheKey collision: (repoPath=org/a_b, no subdir) and (repoPath=org/a, subdir=b) both produced %q", c)
	}
}

func TestIsGitRef(t *testing.T) {
	for ref, want := range map[string]bool{
		"github.com/org/repo//packs/foo@v1": true,
		"gitlab.corp.example/a/b@main":      true,
		"./packs/gitea":                     false,
		"packs/gitea":                       false,
		"oci://ghcr.io/org/pack:v1":         false,
		"git::https://example.com/repo":     false, // explicit getter form, not the bare grammar
		"/abs/path":                         false,
	} {
		if got := isGitRef(ref); got != want {
			t.Fatalf("isGitRef(%q) = %v, want %v", ref, got, want)
		}
	}
}

func TestIsGetterRef(t *testing.T) {
	for ref, want := range map[string]bool{
		"git::https://example.com/repo?ref=v1": true,
		"s3::https://s3.amazonaws.com/b/pack":  true,
		"https://example.com/pack.tar.gz":      true,
		"oci://ghcr.io/org/pack:v1":            false, // stays on the oras path (digest + plain-HTTP)
		"github.com/org/repo//packs/foo@v1":    false, // bare git grammar, translated first
		"./packs/gitea":                        false,
	} {
		if got := isGetterRef(ref); got != want {
			t.Fatalf("isGetterRef(%q) = %v, want %v", ref, got, want)
		}
	}
}

func TestGitRefMustBePinned(t *testing.T) {
	_, err := Fetch(context.Background(), "github.com/org/repo//packs/foo", t.TempDir())
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != "CUBE-4007" {
		t.Fatalf("want CUBE-4007, got %v", err)
	}
}

func TestFetchGitByTag(t *testing.T) {
	fixture := makeGitFixture(t)
	restore := gitCloneURL
	gitCloneURL = func(repoPath string) string { return "file://" + fixture }
	defer func() { gitCloneURL = restore }()

	p, err := Fetch(context.Background(), "example.com/org/repo//packs/demo@v0.1.0", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "demo" || p.Version != "0.1.0" {
		t.Fatalf("metadata: %+v", p)
	}
	if len(p.Pinned) < len("git+")+40 || p.Pinned[:4] != "git+" {
		t.Fatalf("Pinned must be git+<full-sha>, got %q", p.Pinned)
	}
}

// makeGitFixturePlain creates a local repo holding PLAIN Kubernetes
// manifests (no pack.cue) under apps/web, tagged v0.1.0 — the shape of a
// real idpbuilder Application's git source, which was never authored as a
// cube pack.
func makeGitFixturePlain(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git CLI not on PATH")
	}
	dir := t.TempDir()
	appDir := filepath.Join(dir, "apps", "web")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(appDir, "deploy.yaml"),
		[]byte("apiVersion: apps/v1\nkind: Deployment\nmetadata: {name: web}\n"), 0o644)
	for _, args := range [][]string{
		{"init", "-q"}, {"add", "."},
		{"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "init"},
		{"tag", "v0.1.0"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return dir
}

// TestFetchTreePlainManifests pins FetchTree's contract for the cnoe-compat
// loader: a git subdir of plain manifests fetches fine WITHOUT pack.cue
// (Fetch would fail this same tree with CUBE-4003).
func TestFetchTreePlainManifests(t *testing.T) {
	fixture := makeGitFixturePlain(t)
	restore := gitCloneURL
	gitCloneURL = func(repoPath string) string { return "file://" + fixture }
	defer func() { gitCloneURL = restore }()

	dir, err := FetchTree(context.Background(), "example.com/org/repo//apps/web@v0.1.0", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "deploy.yaml")); err != nil {
		t.Fatalf("fetched tree is missing deploy.yaml: %v", err)
	}
	// Same ref through Fetch must still demand pack.cue — the seam is only
	// FetchTree skipping loadMeta, not a loosened pack contract.
	_, err = Fetch(context.Background(), "example.com/org/repo//apps/web@v0.1.0", t.TempDir())
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != diag.CodePackCueInvalid {
		t.Fatalf("Fetch of a plain tree: want %s, got %v", diag.CodePackCueInvalid, err)
	}
}

func TestFetchGitUnknownRevision(t *testing.T) {
	fixture := makeGitFixture(t)
	restore := gitCloneURL
	gitCloneURL = func(repoPath string) string { return "file://" + fixture }
	defer func() { gitCloneURL = restore }()

	_, err := Fetch(context.Background(), "example.com/org/repo//packs/demo@v9.9.9", t.TempDir())
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != "CUBE-4006" {
		t.Fatalf("want CUBE-4006, got %v", err)
	}
}
