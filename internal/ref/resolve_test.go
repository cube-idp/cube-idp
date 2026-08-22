package ref

import (
	"testing"

	"github.com/cube-idp/cube-idp/internal/cubeerr"
)

// TestGrammarRejects covers the references the parser refuses, and the
// deferred schemes it recognizes by name. Both entry points share one
// parser, so both must answer identically — which is the property this
// table exists to hold. References that would reach the network on the
// success path are deliberately absent.
func TestGrammarRejects(t *testing.T) {
	tests := []struct {
		name string
		ref  string
		want cubeerr.Code
	}{
		{"empty reference", "", CodeMalformedRef},
		{"bare relative path", "packs/demo", CodeMalformedRef},
		{"bare absolute path", "/srv/packs/demo", CodeMalformedRef},
		{"bare filename", "pack.cue", CodeMalformedRef},
		{"scheme without location", "https://", CodeMalformedRef},
		{"file url with a host", "file://example.com/packs", CodeMalformedRef},
		{"windows-style path", `C:\packs\demo`, CodeMalformedRef},
		{"plain http is not in the table", "http://example.com/values.yaml", CodeUnsupportedScheme},
		{"ftp", "ftp://example.com/values.yaml", CodeUnsupportedScheme},
		{"git over ssh", "git+ssh://example.com/org/repo.git", CodeUnsupportedScheme},
		{"unknown scheme", "helm://example.com/chart", CodeUnsupportedScheme},
		{"git is recognized but deferred", "git+https://example.com/org/repo.git?ref=v1", CodeGitNotImplemented},
		{"oci is recognized but deferred", "oci://example.com/org/pack:0.1.0", CodeOCINotImplemented},
		{"s3 is recognized but deferred", "s3://bucket/key", CodeS3NotImplemented},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, treeErr := ResolveTree(t.Context(), tt.ref)
			requireCode(t, treeErr, tt.want)

			_, fileErr := ResolveFile(t.Context(), tt.ref)
			requireCode(t, fileErr, tt.want)
		})
	}
}

// TestPinVerify covers the integrity check the recorded pin exists for.
func TestPinVerify(t *testing.T) {
	tests := []struct {
		name string
		got  Pin
		want Pin
		code cubeerr.Code // empty means Verify must return nil
	}{
		{
			name: "identical pins match",
			got:  Pin{Ref: "./demo", Source: "/packs/demo", Digest: "sha256:aa"},
			want: Pin{Ref: "./demo", Source: "/packs/demo", Digest: "sha256:aa"},
		},
		{
			name: "same content from another source still matches",
			got:  Pin{Ref: "./demo", Source: "/tmp/demo", Digest: "sha256:aa"},
			want: Pin{Ref: "./demo", Source: "/packs/demo", Digest: "sha256:aa"},
			code: "",
		},
		{
			name: "changed content is a mismatch",
			got:  Pin{Ref: "./demo", Source: "/packs/demo", Digest: "sha256:bb"},
			want: Pin{Ref: "./demo", Source: "/packs/demo", Digest: "sha256:aa"},
			code: CodePinMismatch,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.got.Verify(tt.want)
			if tt.code == "" {
				if err != nil {
					t.Fatalf("Pin.Verify(%+v) = %v, want nil", tt.want, err)
				}
				return
			}
			requireCode(t, err, tt.code)
		})
	}
}
