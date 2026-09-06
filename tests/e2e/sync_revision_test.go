package e2e

import (
	"fmt"
	"strings"
	"testing"
)

// checkFetchedRevision reports whether a Flux source's
// status.artifact.revision names exactly the pinned commit.
//
// The comparison is on the parsed commit component, never a substring of the
// whole revision. Flux renders a git artifact revision as
// "<ref name>@sha1:<full commit>", and the ref name is attacker-free but not
// inert: a branch could be named after a commit, so a substring match would
// accept a revision whose ref name carries the pin while a different commit was
// actually fetched. An empty or malformed pin is rejected rather than treated
// as "matches anything", which is how a substring check silently passes.
//
// It takes strings and returns an error rather than a *testing.T so the
// rejections are testable as data — the cluster is not needed to prove the
// check discriminates.
func checkFetchedRevision(revision, wantCommit string) error {
	if !fullCommitSHA(wantCommit) {
		return fmt.Errorf("pinned commit %q is not a full 40-character hex sha1", wantCommit)
	}
	refName, digest, ok := strings.Cut(revision, "@")
	if !ok {
		return fmt.Errorf("revision %q is not the documented <ref>@sha1:<commit> shape", revision)
	}
	if refName == "" {
		return fmt.Errorf("revision %q names no ref", revision)
	}
	algorithm, commit, ok := strings.Cut(digest, ":")
	if !ok {
		return fmt.Errorf("revision %q carries no digest algorithm", revision)
	}
	if algorithm != "sha1" {
		return fmt.Errorf("revision %q uses digest algorithm %q, want sha1", revision, algorithm)
	}
	if commit != wantCommit {
		return fmt.Errorf("fetched commit %q is not the pinned %q (revision %q) — "+
			"the fixture branch moved, or the source is not the fixture",
			commit, wantCommit, revision)
	}
	return nil
}

// fullCommitSHA reports whether s is a complete lowercase hex sha1. An
// abbreviated or empty pin is rejected: it would weaken the exact comparison
// the pin exists to provide.
func fullCommitSHA(s string) bool {
	if len(s) != 40 {
		return false
	}
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

// TestCheckFetchedRevision locks the pin check that makes fixture drift
// visible. https://github.com/cube-idp/cube-idp/issues/204 is about a source
// whose fetched content is whatever a mutable third-party branch happens to
// point at; the defence is to pin one commit and compare the fetched revision
// against it exactly.
//
// Exactly is the whole point, so the rows below are mostly the ways a looser
// check would wrongly pass. A substring match is the obvious wrong
// implementation: an empty pin is a substring of everything, and a commit that
// appears in the revision's ref-name half is not the commit that was fetched.
//
// The podinfo row is not synthetic. It is the revision the pre-fix e2e actually
// fetched, recorded in evidence/202/red-202.txt — a third-party branch head,
// which is the reported defect.
func TestCheckFetchedRevision(t *testing.T) {
	const pinned = syncFixtureCommit
	tests := []struct {
		name     string
		revision string
		want     string
		wantErr  bool
	}{
		{
			name:     "exact match",
			revision: "e2e/sync-fixture@sha1:" + pinned,
			want:     pinned,
		},
		{
			name:     "the unpinned third-party head the pre-fix suite fetched",
			revision: "master@sha1:dd507173b7b75b2312a36cabe0de5f09c1ce69c8",
			want:     pinned,
			wantErr:  true,
		},
		{
			name:     "ref name contains the pinned commit, fetched commit does not",
			revision: pinned + "@sha1:dd507173b7b75b2312a36cabe0de5f09c1ce69c8",
			want:     pinned,
			wantErr:  true,
		},
		{
			name:     "commit differs in the last character only",
			revision: "e2e/sync-fixture@sha1:1f85328936ab1a602e636415130c0e20b20633c5",
			want:     pinned,
			wantErr:  true,
		},
		{
			name:     "fetched commit merely starts with the pin",
			revision: "e2e/sync-fixture@sha1:" + pinned + "0",
			want:     pinned,
			wantErr:  true,
		},
		{
			name:     "abbreviated commit is not the pinned commit",
			revision: "e2e/sync-fixture@sha1:1f85328",
			want:     pinned,
			wantErr:  true,
		},
		{
			name:     "empty pin matches nothing",
			revision: "e2e/sync-fixture@sha1:" + pinned,
			want:     "",
			wantErr:  true,
		},
		{
			name:     "revision absent — the source never reported an artifact",
			revision: "",
			want:     pinned,
			wantErr:  true,
		},
		{
			name:     "no digest separator",
			revision: "e2e/sync-fixture",
			want:     pinned,
			wantErr:  true,
		},
		{
			name:     "digest algorithm missing",
			revision: "e2e/sync-fixture@" + pinned,
			want:     pinned,
			wantErr:  true,
		},
		{
			name:     "unexpected digest algorithm",
			revision: "e2e/sync-fixture@sha256:" + pinned,
			want:     pinned,
			wantErr:  true,
		},
		{
			name:     "ref name empty",
			revision: "@sha1:" + pinned,
			want:     pinned,
			wantErr:  true,
		},
		{
			name:     "commit empty",
			revision: "e2e/sync-fixture@sha1:",
			want:     pinned,
			wantErr:  true,
		},
		{
			name:     "legacy slash-separated form is not the documented shape",
			revision: "e2e/sync-fixture/" + pinned,
			want:     pinned,
			wantErr:  true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := checkFetchedRevision(tt.revision, tt.want)
			if tt.wantErr && err == nil {
				t.Errorf("checkFetchedRevision(%q, %q) = nil, want a rejection", tt.revision, tt.want)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("checkFetchedRevision(%q, %q) = %v, want no error", tt.revision, tt.want, err)
			}
		})
	}
}
