package cluster

import (
	"errors"
	"fmt"
	"io/fs"
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

// TestKubeconfigReadRemediation pins CUBE-CLU-006's cause-specific
// guidance. One code covers every read-side failure, but the two ways a
// read fails want opposite advice: an absent file is a missing
// prerequisite (`create` has not run), while an unreadable or malformed
// one is a problem with the file that is already there — telling that
// operator to run `create` would send them the wrong way. The location
// failure is a third shape entirely: no file is named, so neither
// remediation applies.
func TestKubeconfigReadRemediation(t *testing.T) {
	t.Parallel()

	readCases := []struct {
		name       string
		err        error
		wantSubstr []string
		notSubstr  []string
	}{
		{
			name:       "absent file gets prerequisite guidance",
			err:        NewKubeconfigReadError(fmt.Errorf("read kubeconfig /nope: %w", fs.ErrNotExist)),
			wantSubstr: []string{"cube-idp create", "--kubeconfig"},
			notSubstr:  []string{"a valid kubeconfig"},
		},
		{
			name:       "unreadable file gets file guidance",
			err:        NewKubeconfigReadError(errors.New("read kubeconfig /x: permission denied")),
			wantSubstr: []string{"a valid kubeconfig", "--kubeconfig"},
			notSubstr:  []string{"cube-idp create"},
		},
		{
			name:       "unresolvable location names KUBECONFIG",
			err:        newKubeconfigLocationError(errors.New("determine default kubeconfig location: $HOME is not defined")),
			wantSubstr: []string{"KUBECONFIG", "--kubeconfig"},
			notSubstr:  []string{"cube-idp create", "a valid kubeconfig"},
		},
	}

	for _, tt := range readCases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var coded *cubeerr.Coded
			if !errors.As(tt.err, &coded) {
				t.Fatalf("err = %v, want a *cubeerr.Coded", tt.err)
			}
			if coded.Code != CodeKubeconfigReadFailed {
				t.Fatalf("Code = %s, want %s", coded.Code, CodeKubeconfigReadFailed)
			}
			// CUBE-CLU-005's words are the defect #203 reports: a
			// read-only verb must never claim an update failed or offer
			// to write elsewhere.
			if strings.Contains(coded.Summary, "update failed") {
				t.Errorf("Summary = %q, must not claim an update failed", coded.Summary)
			}
			if strings.Contains(coded.Remediation, "write elsewhere") {
				t.Errorf("remediation = %q, must not offer to write", coded.Remediation)
			}
			for _, s := range tt.wantSubstr {
				if !strings.Contains(coded.Remediation, s) {
					t.Errorf("remediation must name %q:\n%s", s, coded.Remediation)
				}
			}
			for _, s := range tt.notSubstr {
				if strings.Contains(coded.Remediation, s) {
					t.Errorf("remediation must not name %q:\n%s", s, coded.Remediation)
				}
			}
		})
	}
}
