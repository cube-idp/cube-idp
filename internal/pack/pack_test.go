package pack_test

import (
	"context"
	"errors"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/cube-idp/cube-idp/internal/cubeerr"
	"github.com/cube-idp/cube-idp/internal/pack"
)

// rawPack is the minimal valid raw pack every load case starts from.
func rawPack() fstest.MapFS {
	return fstest.MapFS{
		"pack.cue": &fstest.MapFile{Data: []byte(`name:    "hello"
version: "0.1.0"
type:    "raw"
`)},
		"manifests/ns.yaml": &fstest.MapFile{Data: []byte(`apiVersion: v1
kind: Namespace
metadata:
  name: hello
`)},
	}
}

// wantCode asserts the error carries exactly the expected coded identity.
// Identity is the code, reached with errors.As — never the message text.
func wantCode(t *testing.T, err error, want cubeerr.Code) {
	t.Helper()
	if err == nil {
		t.Fatalf("got nil error, want %s", want)
	}
	var coded *cubeerr.Coded
	if !errors.As(err, &coded) {
		t.Fatalf("error %v is not a *cubeerr.Coded, want %s", err, want)
	}
	if coded.Code != want {
		t.Errorf("error code = %s, want %s", coded.Code, want)
	}
	if coded.Remediation == "" {
		t.Errorf("error %s carries no remediation", coded.Code)
	}
}

func TestLoadValid(t *testing.T) {
	tests := []struct {
		name  string
		files fstest.MapFS
		want  pack.Metadata
	}{
		{
			name:  "raw pack with required fields only",
			files: rawPack(),
			want:  pack.Metadata{Name: "hello", Version: "0.1.0", Type: pack.TypeRaw},
		},
		{
			name: "every optional field set",
			files: fstest.MapFS{
				"pack.cue": &fstest.MapFile{Data: []byte(`name:      "monitoring"
version:   "2.1.0"
type:      "raw"
namespace: "observability"
category:  "gateway"
`)},
				"manifests/x.yaml": &fstest.MapFile{Data: []byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: x\n")},
			},
			want: pack.Metadata{
				Name: "monitoring", Version: "2.1.0", Type: pack.TypeRaw,
				Namespace: "observability", Category: "gateway",
			},
		},
		{
			name: "kustomize payload marker",
			files: fstest.MapFS{
				"pack.cue":            &fstest.MapFile{Data: []byte("name: \"k\"\nversion: \"1\"\ntype: \"kustomize\"\n")},
				"kustomization.yaml":  &fstest.MapFile{Data: []byte("resources: []\n")},
				"manifests/keep.yaml": &fstest.MapFile{Data: []byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: k\n")},
			},
			want: pack.Metadata{Name: "k", Version: "1", Type: pack.TypeKustomize},
		},
		{
			name: "helm payload marker",
			files: fstest.MapFS{
				"pack.cue":   &fstest.MapFile{Data: []byte("name: \"h\"\nversion: \"1\"\ntype: \"helm\"\n")},
				"Chart.yaml": &fstest.MapFile{Data: []byte("name: h\n")},
			},
			want: pack.Metadata{Name: "h", Version: "1", Type: pack.TypeHelm},
		},
		{
			name: "declaring #Values does not break the closed schema",
			files: fstest.MapFS{
				"pack.cue": &fstest.MapFile{Data: []byte(`name:    "v"
version: "1"
type:    "kustomize"
#Values: {replicas: int | *1}
`)},
				"kustomization.yaml": &fstest.MapFile{Data: []byte("resources: []\n")},
			},
			want: pack.Metadata{Name: "v", Version: "1", Type: pack.TypeKustomize},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := pack.Load(t.Context(), tt.files, "./p")
			if err != nil {
				t.Fatalf("Load(%s) = error %v, want a pack", tt.name, err)
			}
			if got := p.Metadata(); got != tt.want {
				t.Errorf("Load(%s).Metadata() = %+v, want %+v", tt.name, got, tt.want)
			}
		})
	}
}

func TestLoadErrors(t *testing.T) {
	tests := []struct {
		name  string
		files fstest.MapFS
		want  cubeerr.Code
	}{
		{
			name:  "no pack.cue at the root",
			files: fstest.MapFS{"manifests/x.yaml": &fstest.MapFile{Data: []byte("{}")}},
			want:  pack.CodeSourceUnreadable,
		},
		{
			name:  "pack.cue does not compile",
			files: fstest.MapFS{"pack.cue": &fstest.MapFile{Data: []byte(`name: "unterminated`)}},
			want:  pack.CodeMetadataCompile,
		},
		{
			name:  "name missing",
			files: fstest.MapFS{"pack.cue": &fstest.MapFile{Data: []byte("version: \"1\"\ntype: \"raw\"\n")}},
			want:  pack.CodeMetadataSchema,
		},
		{
			name:  "version missing",
			files: fstest.MapFS{"pack.cue": &fstest.MapFile{Data: []byte("name: \"x\"\ntype: \"raw\"\n")}},
			want:  pack.CodeMetadataSchema,
		},
		{
			name:  "type missing",
			files: fstest.MapFS{"pack.cue": &fstest.MapFile{Data: []byte("name: \"x\"\nversion: \"1\"\n")}},
			want:  pack.CodeMetadataSchema,
		},
		{
			name:  "type is not one of raw|helm|kustomize",
			files: fstest.MapFS{"pack.cue": &fstest.MapFile{Data: []byte("name: \"x\"\nversion: \"1\"\ntype: \"docker\"\n")}},
			want:  pack.CodeMetadataSchema,
		},
		{
			name:  "undeclared top-level field is rejected by the closed schema",
			files: fstest.MapFS{"pack.cue": &fstest.MapFile{Data: []byte("name: \"x\"\nversion: \"1\"\ntype: \"raw\"\nbogus: true\n")}},
			want:  pack.CodeMetadataSchema,
		},
		{
			name:  "dependsOn is not a pack.cue field",
			files: fstest.MapFS{"pack.cue": &fstest.MapFile{Data: []byte("name: \"x\"\nversion: \"1\"\ntype: \"raw\"\ndependsOn: [\"other\"]\n")}},
			want:  pack.CodeMetadataSchema,
		},
		{
			name:  "uuid is not a pack.cue field",
			files: fstest.MapFS{"pack.cue": &fstest.MapFile{Data: []byte("name: \"x\"\nversion: \"1\"\ntype: \"raw\"\nuuid: \"b2c1\"\n")}},
			want:  pack.CodeMetadataSchema,
		},
		{
			name:  "name is not DNS-label shaped",
			files: fstest.MapFS{"pack.cue": &fstest.MapFile{Data: []byte("name: \"Not A Label\"\nversion: \"1\"\ntype: \"raw\"\n")}},
			want:  pack.CodeMetadataSchema,
		},
		{
			name:  "version is empty",
			files: fstest.MapFS{"pack.cue": &fstest.MapFile{Data: []byte("name: \"x\"\nversion: \"\"\ntype: \"raw\"\n")}},
			want:  pack.CodeMetadataSchema,
		},
		{
			name:  "pack.cue is not a struct",
			files: fstest.MapFS{"pack.cue": &fstest.MapFile{Data: []byte(`"just a string"`)}},
			want:  pack.CodeMetadataSchema,
		},
		{
			name:  "raw pack without a manifests directory",
			files: fstest.MapFS{"pack.cue": &fstest.MapFile{Data: []byte("name: \"x\"\nversion: \"1\"\ntype: \"raw\"\n")}},
			want:  pack.CodePayloadMismatch,
		},
		{
			name: "kustomize pack without a kustomization file",
			files: fstest.MapFS{
				"pack.cue":         &fstest.MapFile{Data: []byte("name: \"x\"\nversion: \"1\"\ntype: \"kustomize\"\n")},
				"manifests/x.yaml": &fstest.MapFile{Data: []byte("{}")},
			},
			want: pack.CodePayloadMismatch,
		},
		{
			name: "helm pack without a chart",
			files: fstest.MapFS{
				"pack.cue":         &fstest.MapFile{Data: []byte("name: \"x\"\nversion: \"1\"\ntype: \"helm\"\n")},
				"manifests/x.yaml": &fstest.MapFile{Data: []byte("{}")},
			},
			want: pack.CodePayloadMismatch,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := pack.Load(t.Context(), tt.files, "./p")
			wantCode(t, err, tt.want)
		})
	}
}

// A raw pack whose manifests entry is a file, not a directory, must be a
// payload mismatch rather than a walk failure later on.
func TestLoadRawManifestsIsAFile(t *testing.T) {
	files := fstest.MapFS{
		"pack.cue":  &fstest.MapFile{Data: []byte("name: \"x\"\nversion: \"1\"\ntype: \"raw\"\n")},
		"manifests": &fstest.MapFile{Data: []byte("not a directory")},
	}
	_, err := pack.Load(t.Context(), files, "./p")
	wantCode(t, err, pack.CodePayloadMismatch)
}

// Load must honour a cancelled context before it reads anything.
func TestLoadCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := pack.Load(ctx, rawPack(), "./p")
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Load(cancelled ctx) error = %v, want context.Canceled", err)
	}
}

// The pack reads only the injected filesystem: nothing outside it is
// reachable, so a pack.cue elsewhere on disk can never be picked up.
func TestLoadReadsOnlyInjectedFS(t *testing.T) {
	var _ fs.FS = rawPack()

	if _, err := pack.Load(t.Context(), fstest.MapFS{}, "./empty"); err == nil {
		t.Fatal("Load(empty fs) = nil error, want CUBE-PKG-001")
	}
}
