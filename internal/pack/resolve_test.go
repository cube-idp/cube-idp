package pack

import (
	"context"
	"testing"
)

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
