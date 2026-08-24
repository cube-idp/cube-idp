package pack_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/cube-idp/cube-idp/internal/pack"
)

// newDir is a path inside the test's own directory that does not exist yet,
// which is the only kind of target pack.New accepts.
func newDir(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join(t.TempDir(), name)
}

// fileRefTo is the reference spelling of a path on disk. --from resolves
// through the reference grammar, which recognises explicit forms only.
func fileRefTo(dir string) string { return "file://" + filepath.ToSlash(dir) }

// renderDir loads a created pack and renders it, returning "kind/name" per
// object. A scaffold that does not render is not a scaffold — this is the
// round trip the command promises.
func renderDir(t *testing.T, dir string) []string {
	t.Helper()
	p, err := pack.Load(t.Context(), os.DirFS(dir), dir)
	if err != nil {
		t.Fatalf("Load(%s) = error %v, want a pack", dir, err)
	}
	plan, err := p.Render(t.Context(), pack.RenderOptions{})
	if err != nil {
		t.Fatalf("Render(%s) = error %v, want a plan", dir, err)
	}

	names := make([]string, 0, len(plan.Objects))
	for _, obj := range plan.Objects {
		names = append(names, obj.GetKind()+"/"+obj.GetName())
	}
	return names
}

// Every scaffold renders as written: that is what makes `pack new` real rather
// than a directory of placeholders an author has to repair first.
func TestNewScaffoldRenders(t *testing.T) {
	tests := []struct {
		name     string
		packType pack.Type
		want     []string
	}{
		{name: "raw", packType: pack.TypeRaw, want: []string{"ConfigMap/hello"}},
		{name: "kustomize", packType: pack.TypeKustomize, want: []string{"ConfigMap/hello"}},
		{name: "empty type scaffolds raw", packType: "", want: []string{"ConfigMap/hello"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := newDir(t, "hello")
			if err := pack.New(t.Context(), pack.NewOptions{Dir: dir, Type: tt.packType}); err != nil {
				t.Fatalf("New() = error %v, want a pack", err)
			}
			if got := renderDir(t, dir); !equalNames(got, tt.want) {
				t.Errorf("rendered %v, want %v", got, tt.want)
			}
		})
	}
}

// The name comes from the directory unless the author says otherwise, because
// `pack new ./traefik` means a pack called traefik.
func TestNewName(t *testing.T) {
	tests := []struct {
		name string
		dir  string
		flag string
		want string
	}{
		{name: "defaults to the directory", dir: "traefik", want: "traefik"},
		{name: "an explicit name wins", dir: "traefik", flag: "gateway", want: "gateway"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := newDir(t, tt.dir)
			if err := pack.New(t.Context(), pack.NewOptions{Dir: dir, Name: tt.flag}); err != nil {
				t.Fatalf("New() = error %v, want a pack", err)
			}
			p, err := pack.Load(t.Context(), os.DirFS(dir), dir)
			if err != nil {
				t.Fatalf("Load() = error %v, want a pack", err)
			}
			if got := p.Metadata().Name; got != tt.want {
				t.Errorf("pack name = %q, want %q", got, tt.want)
			}
		})
	}
}

// A pack is created, never merged into whatever is already at the target.
func TestNewRefusesExistingTarget(t *testing.T) {
	dir := newDir(t, "taken")
	if err := os.Mkdir(dir, 0o750); err != nil {
		t.Fatal(err)
	}

	err := pack.New(t.Context(), pack.NewOptions{Dir: dir})
	wantCode(t, err, pack.CodeTargetExists)
}

// A rejected name is reported before anything is written, so a failed run
// leaves no directory to clean up or to mistake for a half-made pack.
func TestNewValidatesBeforeWriting(t *testing.T) {
	dir := newDir(t, "Not_A_Label")

	err := pack.New(t.Context(), pack.NewOptions{Dir: dir})
	wantCode(t, err, pack.CodeMetadataSchema)

	if _, err := os.Stat(dir); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Stat(%s) = %v, want the directory not to exist", dir, err)
	}
}

// helm renders, but scaffolding one would mean inventing chart coordinates —
// a pack pointing at a chart that does not exist is no more checkable than an
// unrenderable one. It is refused until --from-chart can read real ones.
func TestNewRefusesUnscaffoldableType(t *testing.T) {
	err := pack.New(t.Context(), pack.NewOptions{Dir: newDir(t, "chart"), Type: pack.TypeHelm})
	wantCode(t, err, pack.CodeScaffoldFailed)
}

func equalNames(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
