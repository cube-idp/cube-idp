package pack_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/cube-idp/cube-idp/internal/cubeerr"
	"github.com/cube-idp/cube-idp/internal/pack"
	"github.com/cube-idp/cube-idp/internal/ref"
)

// A fork copies the source pack and renames it, and the copy renders.
func TestNewForkCopiesSource(t *testing.T) {
	source := newDir(t, "source")
	if err := pack.New(t.Context(), pack.NewOptions{Dir: source, Type: pack.TypeKustomize}); err != nil {
		t.Fatalf("New(source) = error %v, want a pack", err)
	}

	forked := newDir(t, "forked")
	if err := pack.New(t.Context(), pack.NewOptions{
		Dir: forked, Name: "forked", From: fileRefTo(source),
	}); err != nil {
		t.Fatalf("New(fork) = error %v, want a pack", err)
	}

	p, err := pack.Load(t.Context(), os.DirFS(forked), forked)
	if err != nil {
		t.Fatalf("Load(fork) = error %v, want a pack", err)
	}
	if got, want := p.Metadata().Name, "forked"; got != want {
		t.Errorf("forked pack name = %q, want %q", got, want)
	}
	// The type came from the source, not from a flag, and the payload came
	// with it — a fork keeps what it copied.
	if got, want := p.Metadata().Type, pack.TypeKustomize; got != want {
		t.Errorf("forked pack type = %q, want the source's %q", got, want)
	}
	if got := renderDir(t, forked); len(got) == 0 {
		t.Error("forked pack rendered nothing, want the source's objects")
	}
	for _, file := range []string{"pack.cue", "kustomization.yaml", "configmap.yaml"} {
		if _, err := os.Stat(filepath.Join(forked, file)); err != nil {
			t.Errorf("Stat(%s) in the fork = %v, want the file copied", file, err)
		}
	}
}

// Without --name a fork copies, name included. The directory is not a rename
// request: --from means "give me this pack", and renaming happens only when it
// is asked for.
func TestNewForkKeepsSourceNameByDefault(t *testing.T) {
	source := newDir(t, "source")
	if err := pack.New(t.Context(), pack.NewOptions{Dir: source}); err != nil {
		t.Fatalf("New(source) = error %v, want a pack", err)
	}

	forked := filepath.Join(filepath.Dir(source), "some-other-directory")
	if err := pack.New(t.Context(), pack.NewOptions{Dir: forked, From: fileRefTo(source)}); err != nil {
		t.Fatalf("New(fork) = error %v, want a pack", err)
	}

	p, err := pack.Load(t.Context(), os.DirFS(forked), forked)
	if err != nil {
		t.Fatalf("Load(fork) = error %v, want a pack", err)
	}
	if got, want := p.Metadata().Name, "source"; got != want {
		t.Errorf("forked pack name = %q, want the source's %q — the directory is not a rename", got, want)
	}
	if got := renderDir(t, forked); len(got) == 0 {
		t.Error("forked pack rendered nothing, want the source's objects")
	}
}

// Naming the fork what it is already called is not a rename either.
func TestNewForkWithSameName(t *testing.T) {
	source := newDir(t, "source")
	if err := pack.New(t.Context(), pack.NewOptions{Dir: source}); err != nil {
		t.Fatalf("New(source) = error %v, want a pack", err)
	}

	forked := filepath.Join(filepath.Dir(source), "source-copy")
	if err := pack.New(t.Context(), pack.NewOptions{
		Dir: forked, Name: "source", From: fileRefTo(source),
	}); err != nil {
		t.Fatalf("New(fork) = error %v, want a pack", err)
	}
	if got := renderDir(t, forked); len(got) == 0 {
		t.Error("forked pack rendered nothing, want the source's objects")
	}
}

// A pack.cue this cannot rewrite is only a problem when a rename was actually
// requested: forking it as-is has nothing to rewrite and must succeed.
func TestNewForkComputedNameWithoutRename(t *testing.T) {
	source := computedNamePack(t)

	forked := filepath.Join(filepath.Dir(source), "copy")
	if err := pack.New(t.Context(), pack.NewOptions{Dir: forked, From: fileRefTo(source)}); err != nil {
		t.Fatalf("New(fork) = error %v, want the pack copied as it is", err)
	}
	p, err := pack.Load(t.Context(), os.DirFS(forked), forked)
	if err != nil {
		t.Fatalf("Load(fork) = error %v, want a pack", err)
	}
	if got, want := p.Metadata().Name, "computed"; got != want {
		t.Errorf("forked pack name = %q, want %q", got, want)
	}
}

// computedNamePack writes a pack whose pack.cue names itself through a
// reference rather than a plain string — valid CUE that a textual rename
// cannot safely edit.
func computedNamePack(t *testing.T) string {
	t.Helper()
	dir := newDir(t, "computed")
	if err := os.MkdirAll(filepath.Join(dir, "manifests"), 0o750); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "pack.cue"),
		"_base: \"computed\"\nname: _base\nversion: \"1\"\ntype: \"raw\"\n")
	write(t, filepath.Join(dir, "manifests", "cm.yaml"),
		"apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: c\n")
	return dir
}

// A pack.cue that does not spell its name as a plain string cannot be renamed
// safely, and the fork is refused rather than delivered under the source's
// name — two packs sharing a name is an identity collision downstream.
func TestNewForkRenameRefused(t *testing.T) {
	source := computedNamePack(t)

	target := filepath.Join(filepath.Dir(source), "renamed")
	err := pack.New(t.Context(), pack.NewOptions{
		Dir: target, Name: "renamed", From: fileRefTo(source),
	})
	wantCode(t, err, pack.CodeScaffoldFailed)

	if _, statErr := os.Stat(target); !errors.Is(statErr, fs.ErrNotExist) {
		t.Errorf("Stat(%s) = %v, want no directory left behind", target, statErr)
	}
}

// Forking something that is not a pack fails as that, before any copying.
func TestNewForkSourceIsNotAPack(t *testing.T) {
	empty := t.TempDir()

	err := pack.New(t.Context(), pack.NewOptions{Dir: newDir(t, "x"), From: fileRefTo(empty)})
	wantCode(t, err, pack.CodeSourceUnreadable)
}

// A backend this build does not implement keeps the reference leaf's own code:
// the error names which backend is missing and where it lands.
func TestNewForkUnimplementedBackend(t *testing.T) {
	tests := []struct {
		name string
		from string
		want cubeerr.Code
	}{
		{name: "oci", from: "oci://example.com/pack:1", want: ref.CodeOCINotImplemented},
		{name: "git", from: "git+https://example.com/o/r.git?ref=main", want: ref.CodeGitNotImplemented},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := pack.New(t.Context(), pack.NewOptions{Dir: newDir(t, "x"), From: tt.from})
			wantCode(t, err, tt.want)
		})
	}
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
