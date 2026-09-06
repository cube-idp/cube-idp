//go:build unix

package cli

import "testing"

// metadataDisplayPath names a directory's pack.cue without respelling the
// directory. The rows are the boundaries where a naive implementation goes
// wrong: filepath.Join would strip the leading "./" of the first row, and a
// literal `"/\\"` cutset would eat the trailing backslash of the last, which
// on this platform is part of the directory's name.
//
// Every expectation here spells the separator "/" and the last row treats `\`
// as an ordinary filename character, both of which are Unix facts
// (os/path_unix.go vs os/path_windows.go). The build constraint above is what
// keeps that scope honest — a comment cannot enforce it, and these rows would
// fail on Windows for reasons that say nothing about the helper.
func TestMetadataDisplayPath(t *testing.T) {
	tests := []struct {
		name string
		dir  string
		want string
	}{
		{name: "the reported bug", dir: "./podinfo", want: "./podinfo/pack.cue"},
		{name: "bare relative gains no prefix", dir: "podinfo", want: "podinfo/pack.cue"},
		{name: "trailing separator is not doubled", dir: "podinfo/", want: "podinfo/pack.cue"},
		{name: "repeated trailing separators", dir: "./podinfo//", want: "./podinfo/pack.cue"},
		{name: "parent-relative survives", dir: "../podinfo", want: "../podinfo/pack.cue"},
		{name: "degenerate directory", dir: ".", want: "./pack.cue"},
		{name: "absolute is unchanged", dir: "/abs/podinfo", want: "/abs/podinfo/pack.cue"},
		{name: "absolute with trailing separator", dir: "/abs/podinfo/", want: "/abs/podinfo/pack.cue"},
		{name: "root", dir: "/", want: "/pack.cue"},
		{name: "a backslash is a filename character here", dir: `literal\`, want: `literal\/pack.cue`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := metadataDisplayPath(tt.dir); got != tt.want {
				t.Errorf("metadataDisplayPath(%q) = %q, want %q", tt.dir, got, tt.want)
			}
		})
	}
}
