package pack

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// upgrade --plan compares ResolveRemote's would-be pin against the pin
// cube.lock recorded via FetchFile. For a local single-file ref (valuesRef,
// providerConfigRef, remote -f) that means both must produce the SAME
// file:<sha256-hex> — a dirhash here would report permanent drift.
func TestResolveRemoteLocalFileMatchesFetchFile(t *testing.T) {
	f := filepath.Join(t.TempDir(), "values.yaml")
	if err := os.WriteFile(f, []byte("replicas: 3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, want, err := FetchFile(context.Background(), f, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	pin, err := ResolveRemote(context.Background(), f, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if pin != want {
		t.Fatalf("ResolveRemote %q != FetchFile pin %q", pin, want)
	}
	if len(pin) < 5 || pin[:5] != "file:" {
		t.Fatalf("want file:<sha256>, got %q", pin)
	}
}

func TestResolveRemoteLocalDirMatchesFetch(t *testing.T) {
	p, err := Fetch(context.Background(), "testdata/demo", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	pin, err := ResolveRemote(context.Background(), "testdata/demo", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if pin != p.Pinned {
		t.Fatalf("ResolveRemote %q != Fetch pin %q", pin, p.Pinned)
	}
}

func TestResolveRemoteGitTag(t *testing.T) {
	fixture := makeGitFixture(t) // from getter_test.go
	restore := gitCloneURL
	gitCloneURL = func(string) string { return "file://" + fixture }
	defer func() { gitCloneURL = restore }()

	pin, err := ResolveRemote(context.Background(), "example.com/org/repo//packs/demo@v0.1.0", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(pin) < 4 || pin[:4] != "git+" {
		t.Fatalf("want git+<sha>, got %q", pin)
	}
}
