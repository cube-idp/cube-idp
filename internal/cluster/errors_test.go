package cluster

import (
	"errors"
	"strings"
	"testing"

	"github.com/cube-idp/cube-idp/internal/cubeerr"
)

// TestProvisionFailedRemediation pins the operator guidance CUBE-CLU-004
// carries per action. Error *identity* is asserted the usual way (errors.As
// + Code); the remediation *text* is asserted directly because the text is
// the defect this test exists for: on a `create`, the failure an operator
// actually hits is a host-port collision between two default cubes, and
// docs/domains/cluster.md:99-103 names `spec.cluster.forProvider` as the
// escape. The port numbers themselves stay in the kind driver's contract
// (cluster.md:84-88) and are deliberately not respelled here.
func TestProvisionFailedRemediation(t *testing.T) {
	t.Parallel()

	remediationCases := []struct {
		action string
		// wantSubstr are the concepts the guidance must name; notSubstr the
		// ones it must not, so a single broadened string cannot satisfy
		// every action at once. Phrases, not bare words: "port" alone
		// matches "support", which would make both directions unreliable.
		wantSubstr []string
		notSubstr  []string
	}{
		{
			action:     "create",
			wantSubstr: []string{"runtime", "host ports", "spec.cluster.forProvider"},
		},
		{
			action:     "list",
			wantSubstr: []string{"runtime"},
			notSubstr:  []string{"host ports", "spec.cluster.forProvider"},
		},
		{
			action:     "delete",
			wantSubstr: []string{"runtime"},
			notSubstr:  []string{"host ports", "spec.cluster.forProvider"},
		},
	}

	for _, tt := range remediationCases {
		t.Run(tt.action, func(t *testing.T) {
			t.Parallel()
			err := NewProvisionFailedError(tt.action, "demo2", errors.New("exit status 125"))

			var coded *cubeerr.Coded
			if !errors.As(err, &coded) {
				t.Fatalf("err = %v, want a *cubeerr.Coded", err)
			}
			if coded.Code != CodeProvisionFailed {
				t.Fatalf("Code = %s, want %s", coded.Code, CodeProvisionFailed)
			}
			if want := `create cluster "demo2" failed`; tt.action == "create" && coded.Summary != want {
				t.Errorf("Summary = %q, want %q", coded.Summary, want)
			}
			for _, s := range tt.wantSubstr {
				if !strings.Contains(coded.Remediation, s) {
					t.Errorf("remediation for %q must name %q:\n%s", tt.action, s, coded.Remediation)
				}
			}
			for _, s := range tt.notSubstr {
				if strings.Contains(coded.Remediation, s) {
					t.Errorf("remediation for %q must not name %q:\n%s", tt.action, s, coded.Remediation)
				}
			}
		})
	}
}

// TestProvisionFailedPreservesCause: the remediation change must not cost
// the wrapped cause, which is where kind's underlying exec failure shows.
func TestProvisionFailedPreservesCause(t *testing.T) {
	t.Parallel()

	cause := errors.New("exit status 125")
	err := NewProvisionFailedError("create", "demo2", cause)
	if !errors.Is(err, cause) {
		t.Fatalf("cause not reachable through the coded error: %v", err)
	}
}
