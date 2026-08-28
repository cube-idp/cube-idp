package ca_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/cube-idp/cube-idp/internal/ca"
	"github.com/cube-idp/cube-idp/internal/cubeerr"
)

// TestP11KitOperations pins both argv sequences and the two failure
// texts a Linux user actually reads.
func TestP11KitOperations(t *testing.T) {
	t.Parallel()
	certPEM, fingerprint := mintFingerprint(t, testCube, 1)

	probe := []string{"trust", "list", "--filter=ca-anchors"}
	store := []string{"trust", "anchor", "--store", testCertPath}
	remove := []string{"trust", "anchor", "--remove", testCertPath}

	cases := []struct {
		name        string
		remove      bool
		respond     func(name string, args []string) ([]byte, []byte, error)
		wantCalls   [][]string
		wantErr     bool
		wantRemedy  string
		wantMessage string
	}{
		{name: "install probes then stores", wantCalls: [][]string{probe, store}},
		{name: "remove takes the file", remove: true, wantCalls: [][]string{remove}},
		{
			name: "no writable user anchor store",
			respond: func(_ string, args []string) ([]byte, []byte, error) {
				if args[0] == "list" {
					return nil, nil, nil
				}
				return nil, []byte("p11-kit: no configured writable location to store anchors"),
					errors.New("exit status 1")
			},
			wantCalls:   [][]string{probe, store},
			wantErr:     true,
			wantRemedy:  "update-ca-certificates",
			wantMessage: "no configured writable location",
		},
		{
			name:       "trust is not installed",
			respond:    func(string, []string) ([]byte, []byte, error) { return nil, nil, notFound("trust") },
			wantCalls:  [][]string{probe},
			wantErr:    true,
			wantRemedy: "p11-kit",
		},
		{
			name:       "trust is not installed on remove",
			remove:     true,
			respond:    func(string, []string) ([]byte, []byte, error) { return nil, nil, notFound("trust") },
			wantCalls:  [][]string{remove},
			wantErr:    true,
			wantRemedy: "install p11-kit",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runner := &fakeRunner{respond: tc.respond}
			p11 := ca.NewP11KitStore(runner.run)

			var err error
			if tc.remove {
				_, err = p11.Remove(t.Context(), ca.RemoveRequest{
					Cube: testCube, Fingerprint: fingerprint, CertPath: testCertPath, CertPEM: certPEM,
				})
			} else {
				err = p11.Install(t.Context(), testCube, testCertPath)
			}
			if tc.wantErr {
				assertRemediation(t, assertCode(t, err, ca.CodeTrustStore), tc.wantRemedy, tc.wantMessage)
			} else if err != nil {
				t.Fatalf("operation error = %v", err)
			}
			assertCalls(t, runner.calls, tc.wantCalls)
		})
	}
}

// assertRemediation checks the user-facing halves of a coded error: the
// remediation must name the manual remedy, and the cause must carry the
// tool's own explanation rather than a bare exit status.
func assertRemediation(t *testing.T, coded *cubeerr.Coded, wantRemedy, wantCause string) {
	t.Helper()
	if wantRemedy != "" && !strings.Contains(coded.Remediation, wantRemedy) {
		t.Errorf("remediation = %q, want it to name %q", coded.Remediation, wantRemedy)
	}
	if wantCause != "" && !strings.Contains(fmt.Sprint(coded.Unwrap()), wantCause) {
		t.Errorf("cause = %v, want it to carry %q", coded.Unwrap(), wantCause)
	}
}

// TestP11KitRemoveWithoutArtifact: p11-kit can only verify the local
// file, so with no file there is nothing to identify — and an
// unidentified anchor is never removed.
func TestP11KitRemoveWithoutArtifact(t *testing.T) {
	t.Parallel()
	runner := &fakeRunner{}

	_, err := ca.NewP11KitStore(runner.run).Remove(t.Context(), ca.RemoveRequest{
		Cube: testCube, Fingerprint: "fce7a0ea", CertPath: testCertPath,
	})
	_ = assertCode(t, err, ca.CodeTrustStore)
	if len(runner.calls) != 0 {
		t.Errorf("calls = %v, want none — nothing was verified", runner.calls)
	}
}

// TestP11KitRemoveRefusesForeignCertificate: the local artifact is
// verified against both the ledger fingerprint and the marker before
// trust(1) is invoked at all.
func TestP11KitRemoveRefusesForeignCertificate(t *testing.T) {
	t.Parallel()
	foreignPEM, foreignFingerprint := mintFingerprint(t, "other", 1)
	runner := &fakeRunner{}

	_, err := ca.NewP11KitStore(runner.run).Remove(t.Context(), ca.RemoveRequest{
		Cube: testCube, Fingerprint: foreignFingerprint, CertPath: testCertPath, CertPEM: foreignPEM,
	})
	_ = assertCode(t, err, ca.CodeTrustStore)
	if len(runner.calls) != 0 {
		t.Errorf("calls = %v, want none — a refused removal never reaches the tool", runner.calls)
	}
}
