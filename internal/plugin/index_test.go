package plugin

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/rafpe/cube-idp/internal/diag"
)

func tgzWithBin(t *testing.T, binName, content string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	tw.WriteHeader(&tar.Header{Name: binName, Mode: 0o755, Size: int64(len(content))})
	tw.Write([]byte(content))
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func gitIndex(t *testing.T, entryYAML string) string {
	t.Helper()
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "plugins"), 0o755)
	os.WriteFile(filepath.Join(dir, "plugins", "hello.yaml"), []byte(entryYAML), 0o644)
	for _, args := range [][]string{
		{"init", "-q"}, {"add", "-A"},
		{"-c", "user.email=t@t", "-c", "user.name=t", "commit", "-qm", "index"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return dir
}

func TestInstallVerifiesShaAndInstalls(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-only")
	}
	archive := tgzWithBin(t, "cube-idp-hello", "#!/bin/sh\necho hi\n")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write(archive) }))
	t.Cleanup(srv.Close)
	sum := sha256.Sum256(archive)
	entry := fmt.Sprintf(
		"name: hello\nshortDescription: test\nplatforms:\n  - os: %s\n    arch: %s\n    url: %s/a.tgz\n    sha256: %s\n    bin: cube-idp-hello\n",
		runtime.GOOS, runtime.GOARCH, srv.URL, hex.EncodeToString(sum[:]))
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_DATA_HOME", "")

	if err := Install(context.Background(), gitIndex(t, entry), "hello"); err != nil {
		t.Fatal(err)
	}
	p, ok := Lookup("hello")
	if !ok {
		t.Fatal("installed plugin not discoverable")
	}
	if err := EnsureTrusted("hello", p, false); err != nil {
		t.Fatalf("verified install must be pre-trusted: %v", err)
	}
}

func TestInstallRejectsShaMismatch(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-only")
	}
	archive := tgzWithBin(t, "cube-idp-hello", "#!/bin/sh\necho evil\n")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write(archive) }))
	t.Cleanup(srv.Close)
	entry := fmt.Sprintf(
		"name: hello\nshortDescription: test\nplatforms:\n  - os: %s\n    arch: %s\n    url: %s/a.tgz\n    sha256: %s\n    bin: cube-idp-hello\n",
		runtime.GOOS, runtime.GOARCH, srv.URL, "deadbeef"+string(bytes.Repeat([]byte("0"), 56)))
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_DATA_HOME", "")

	err := Install(context.Background(), gitIndex(t, entry), "hello")
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != "CUBE-7102" {
		t.Fatalf("want CUBE-7102 on sha mismatch, got %v", err)
	}
	if _, ok := Lookup("hello"); ok {
		t.Fatal("a sha-mismatched plugin must never land in InstallDir")
	}
}

// TestFetchArchiveTimesOut proves indexHTTPClient enforces a deadline: a
// server that responds far slower than the client timeout must not hang
// Install forever. The package var's Timeout is shrunk for the duration of
// the test (and restored after) so the test itself stays fast. The handler
// sleeps a fixed duration well past the shrunk timeout and then returns
// normally (rather than blocking on an external signal) so
// httptest.Server.Close, which waits for outstanding requests to complete,
// cannot deadlock against the client giving up first.
func TestFetchArchiveTimesOut(t *testing.T) {
	prev := indexHTTPClient.Timeout
	indexHTTPClient.Timeout = 50 * time.Millisecond
	defer func() { indexHTTPClient.Timeout = prev }()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(500 * time.Millisecond) // well past the shrunk client timeout
	}))
	t.Cleanup(srv.Close)

	platform := &Platform{OS: runtime.GOOS, Arch: runtime.GOARCH, URL: srv.URL + "/a.tgz", SHA256: strings.Repeat("0", 64)}
	_, err := fetchArchive(context.Background(), "hello", platform)
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != "CUBE-7102" {
		t.Fatalf("want CUBE-7102 on client timeout, got %v", err)
	}
}

func TestInstallUnknownPlugin(t *testing.T) {
	err := Install(context.Background(), gitIndex(t, "name: hello\nplatforms: []\n"), "absent")
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != "CUBE-7101" {
		t.Fatalf("want CUBE-7101, got %v", err)
	}
}

// TestInstallRejectsOptionInjectionInIndexURL covers the git argument
// injection vector: an indexURL beginning with "-" (e.g. --upload-pack=…)
// must be rejected by validation BEFORE any git process runs — if it ever
// reached `git clone`, git would parse it as an option and, for
// --upload-pack, execute an attacker-chosen command. The marker file
// proves the injected command never ran.
func TestInstallRejectsOptionInjectionInIndexURL(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-only")
	}
	dir := t.TempDir()
	marker := filepath.Join(dir, "pwned")
	script := filepath.Join(dir, "evil.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ntouch "+marker+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	err := Install(context.Background(), "--upload-pack="+script, "hello")
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != "CUBE-7102" {
		t.Fatalf("want CUBE-7102 for an option-shaped index URL, got %v", err)
	}
	if !strings.Contains(err.Error(), "invalid index URL") {
		t.Fatalf("want a validation rejection (not a downstream git failure), got %v", err)
	}
	if _, statErr := os.Stat(marker); statErr == nil {
		t.Fatal("the injected --upload-pack command was executed")
	}
}

// TestInstallRejectsOptionInjectionInPin: a pinned "commit" of "-evil"
// would be parsed by git fetch/checkout as an option; validation must
// reject it before any git subprocess sees it.
func TestInstallRejectsOptionInjectionInPin(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-only")
	}
	repo := gitIndex(t, "name: hello\nplatforms: []\n")
	err := Install(context.Background(), repo+"@-evil", "hello")
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != "CUBE-7102" {
		t.Fatalf("want CUBE-7102 for an option-shaped pin, got %v", err)
	}
	if !strings.Contains(err.Error(), "invalid index pin") {
		t.Fatalf("want a validation rejection (not a downstream git failure), got %v", err)
	}
}

// gitIndexTwoCommits builds an index repo whose first commit carries
// entryV1 and whose HEAD carries entryV2, returning the repo path and the
// first commit's full sha. uploadpack.allowAnySHA1InWant is enabled on the
// fixture so a depth-1 clone can fetch the older (unadvertised) commit —
// real hosting providers (GitHub et al.) allow this for reachable commits.
func gitIndexTwoCommits(t *testing.T, entryV1, entryV2 string) (repo, firstCommit string) {
	t.Helper()
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "plugins"), 0o755)
	run := func(args ...string) string {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	os.WriteFile(filepath.Join(dir, "plugins", "hello.yaml"), []byte(entryV1), 0o644)
	run("init", "-q")
	run("config", "uploadpack.allowAnySHA1InWant", "true")
	run("add", "-A")
	run("-c", "user.email=t@t", "-c", "user.name=t", "commit", "-qm", "v1")
	firstCommit = run("rev-parse", "HEAD")
	os.WriteFile(filepath.Join(dir, "plugins", "hello.yaml"), []byte(entryV2), 0o644)
	run("add", "-A")
	run("-c", "user.email=t@t", "-c", "user.name=t", "commit", "-qm", "v2")
	return dir, firstCommit
}

// TestInstallCommitPinSelectsPinnedDescriptor proves the @<commit> pin is
// the control it claims to be: when a (compromised) index HEAD swaps the
// descriptor to a different sha, installing with the pin must use the
// FIRST commit's descriptor (and succeed against the real archive), while
// installing without the pin must see HEAD's swapped descriptor and be
// refused by sha verification.
func TestInstallCommitPinSelectsPinnedDescriptor(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-only")
	}
	archive := tgzWithBin(t, "cube-idp-hello", "#!/bin/sh\necho hi\n")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write(archive) }))
	t.Cleanup(srv.Close)
	sum := sha256.Sum256(archive)
	entryFor := func(sha string) string {
		return fmt.Sprintf(
			"name: hello\nshortDescription: test\nplatforms:\n  - os: %s\n    arch: %s\n    url: %s/a.tgz\n    sha256: %s\n    bin: cube-idp-hello\n",
			runtime.GOOS, runtime.GOARCH, srv.URL, sha)
	}
	goodEntry := entryFor(hex.EncodeToString(sum[:]))
	swappedEntry := entryFor("deadbeef" + strings.Repeat("0", 56))
	repo, firstCommit := gitIndexTwoCommits(t, goodEntry, swappedEntry)

	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_DATA_HOME", "")

	// Unpinned: HEAD's swapped descriptor pins a bogus sha — verification
	// must refuse it and nothing may land in InstallDir.
	err := Install(context.Background(), repo, "hello")
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != "CUBE-7102" {
		t.Fatalf("unpinned install must see HEAD's (bogus) descriptor and fail sha verification, got %v", err)
	}
	if _, ok := Lookup("hello"); ok {
		t.Fatal("the sha-mismatched HEAD descriptor must not have installed anything")
	}

	// Pinned to the first commit: the original descriptor's sha matches the
	// archive, so the install must succeed.
	if err := Install(context.Background(), repo+"@"+firstCommit, "hello"); err != nil {
		t.Fatalf("pinned install must use the pinned commit's descriptor: %v", err)
	}
	if _, ok := Lookup("hello"); !ok {
		t.Fatal("pinned install did not land in InstallDir")
	}
}
