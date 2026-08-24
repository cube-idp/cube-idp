package pack_test

import (
	"testing"
	"testing/fstest"

	"github.com/cube-idp/cube-idp/internal/cubeerr"
	"github.com/cube-idp/cube-idp/internal/pack"
)

// The chart block is discriminated on kind, so each form admits exactly its
// own fields. A violation is a schema fault, never a silently-ignored field.
func TestHelmChartSchema(t *testing.T) {
	const head = "name: \"a\"\nversion: \"1\"\ntype: \"helm\"\n"

	tests := []struct {
		name string
		cue  string
		want cubeerr.Code
	}{
		{
			name: "repo chart is accepted",
			cue:  head + "chart: {kind: \"repo\", url: \"https://c.example.com\", name: \"a\", version: \"1.0.0\"}\n",
		},
		{
			name: "oci chart is accepted",
			cue:  head + "chart: {kind: \"oci\", url: \"oci://r.example.com/a\", version: \"1.0.0\"}\n",
		},
		{
			name: "helm pack without a chart block",
			cue:  head,
			want: pack.CodeMetadataSchema,
		},
		{
			name: "unknown chart kind",
			cue:  head + "chart: {kind: \"git\", url: \"https://c.example.com\", version: \"1.0.0\"}\n",
			want: pack.CodeMetadataSchema,
		},
		{
			name: "repo chart without a chart name",
			cue:  head + "chart: {kind: \"repo\", url: \"https://c.example.com\", version: \"1.0.0\"}\n",
			want: pack.CodeMetadataSchema,
		},
		{
			name: "name is forbidden on an oci chart",
			cue:  head + "chart: {kind: \"oci\", url: \"oci://r.example.com/a\", name: \"a\", version: \"1.0.0\"}\n",
			want: pack.CodeMetadataSchema,
		},
		{
			name: "digest is forbidden on a repo chart",
			cue:  head + "chart: {kind: \"repo\", url: \"https://c.example.com\", name: \"a\", version: \"1.0.0\", digest: \"sha256:abc\"}\n",
			want: pack.CodeMetadataSchema,
		},
		{
			name: "repo url must not be an oci reference",
			cue:  head + "chart: {kind: \"repo\", url: \"oci://r.example.com/a\", name: \"a\", version: \"1.0.0\"}\n",
			want: pack.CodeMetadataSchema,
		},
		{
			name: "oci url must be an oci reference",
			cue:  head + "chart: {kind: \"oci\", url: \"https://c.example.com\", version: \"1.0.0\"}\n",
			want: pack.CodeMetadataSchema,
		},
		{
			name: "malformed digest",
			cue:  head + "chart: {kind: \"oci\", url: \"oci://r.example.com/a\", version: \"1.0.0\", digest: \"sha256:zzz\"}\n",
			want: pack.CodeMetadataSchema,
		},
		{
			name: "a chart on a raw pack has no field to unify with",
			cue:  "name: \"a\"\nversion: \"1\"\ntype: \"raw\"\n" + chartCUE,
			want: pack.CodeMetadataSchema,
		},
		{
			name: "a chart on a kustomize pack has no field to unify with",
			cue:  "name: \"a\"\nversion: \"1\"\ntype: \"kustomize\"\n" + chartCUE,
			want: pack.CodeMetadataSchema,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := pack.Load(t.Context(), helmPack(tt.cue), "./p")
			if tt.want == "" {
				if err != nil {
					t.Fatalf("Load(%s) = error %v, want a pack", tt.name, err)
				}
				return
			}
			wantCode(t, err, tt.want)
		})
	}
}

// A chart version must be one exact SemVer. Ranges, partial versions, a
// leading v, and build metadata are all rejected — the first three by the CUE
// shape check, the last by the parser that is the actual authority.
func TestHelmChartVersion(t *testing.T) {
	const head = "name: \"a\"\nversion: \"1\"\ntype: \"helm\"\n"

	tests := []struct {
		name    string
		version string
		wantErr bool
	}{
		{name: "exact version", version: "6.5.4"},
		{name: "prerelease", version: "1.2.3-rc.1"},
		{name: "leading v", version: "v6.5.4", wantErr: true},
		{name: "partial version", version: "6.5", wantErr: true},
		{name: "major only", version: "6", wantErr: true},
		{name: "range", version: ">=1.0.0", wantErr: true},
		{name: "wildcard", version: "6.5.*", wantErr: true},
		{name: "build metadata", version: "1.2.3+meta", wantErr: true},
		{name: "empty", version: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cue := head + "chart: {kind: \"repo\", url: \"https://c.example.com\", name: \"a\", version: \"" + tt.version + "\"}\n"
			_, err := pack.Load(t.Context(), helmPack(cue), "./p")
			if !tt.wantErr {
				if err != nil {
					t.Fatalf("Load(version=%q) = error %v, want a pack", tt.version, err)
				}
				return
			}
			wantCode(t, err, pack.CodeMetadataSchema)
		})
	}
}

// A helm pack is thin, so chart content at its root is the payload mismatch —
// read from the opposite direction to raw and kustomize, and rejected rather
// than ignored.
func TestHelmBundledChartRejected(t *testing.T) {
	const cue = "name: \"a\"\nversion: \"1\"\ntype: \"helm\"\n" + chartCUE

	tests := []struct {
		name  string
		files fstest.MapFS
	}{
		{
			name:  "Chart.yaml",
			files: fstest.MapFS{"Chart.yaml": &fstest.MapFile{Data: []byte("name: a\n")}},
		},
		{
			name:  "Chart.yml",
			files: fstest.MapFS{"Chart.yml": &fstest.MapFile{Data: []byte("name: a\n")}},
		},
		{
			name:  "templates directory",
			files: fstest.MapFS{"templates/deploy.yaml": &fstest.MapFile{Data: []byte("{}\n")}},
		},
		{
			name:  "charts directory",
			files: fstest.MapFS{"charts/sub/Chart.yaml": &fstest.MapFile{Data: []byte("name: sub\n")}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files := fstest.MapFS{"pack.cue": &fstest.MapFile{Data: []byte(cue)}}
			for name, file := range tt.files {
				files[name] = file
			}
			_, err := pack.Load(t.Context(), files, "./p")
			wantCode(t, err, pack.CodePayloadMismatch)
		})
	}
}
