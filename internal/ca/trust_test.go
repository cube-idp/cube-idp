package ca_test

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"testing"

	"github.com/cube-idp/cube-idp/internal/ca"
)

const (
	testKeychain = "/Users/tester/Library/Keychains/login.keychain-db"
	testCertPath = "/home/tester/.cube-idp/demo/ca.crt"
)

// fakeRunner is the injected trust-tool seam: it records every argv and
// answers from a canned function, so the drivers are exercised without
// an OS trust store anywhere near the gate.
type fakeRunner struct {
	calls   [][]string
	respond func(name string, args []string) (stdout, stderr []byte, err error)
}

func (f *fakeRunner) run(_ context.Context, name string, args ...string) ([]byte, []byte, error) {
	f.calls = append(f.calls, append([]string{name}, args...))
	if f.respond == nil {
		return nil, nil, nil
	}
	return f.respond(name, args)
}

// notFound is what a Runner reports when the tool is not installed: the
// contract on ca.Runner is os/exec's own sentinel.
func notFound(tool string) error {
	return &exec.Error{Name: tool, Err: exec.ErrNotFound}
}

// keychainAnswer replies to `security login-keychain` the way the real
// tool does — indented and quoted — and hands every other call to next.
func keychainAnswer(next func(name string, args []string) ([]byte, []byte, error)) func(string, []string) ([]byte, []byte, error) {
	return func(name string, args []string) ([]byte, []byte, error) {
		if len(args) == 1 && args[0] == "login-keychain" {
			return []byte(fmt.Sprintf("    %q\n", testKeychain)), nil, nil
		}
		if next == nil {
			return nil, nil, nil
		}
		return next(name, args)
	}
}

// findOutput renders `security find-certificate -a -p -Z` output: a
// SHA-256 line, the SHA-1 line the driver must ignore, then the PEM.
func findOutput(fingerprint string, certPEM []byte) []byte {
	return []byte(fmt.Sprintf("SHA-256 hash: %s\nSHA-1 hash: 91416107AABB\n%s",
		strings.ToUpper(fingerprint), certPEM))
}

// assertCalls asserts the exact argv sequence a driver produced — the
// point of the injected runner is that the commands are the contract.
func assertCalls(t *testing.T, got, want [][]string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("calls = %v, want %v", got, want)
	}
	for i := range want {
		if strings.Join(got[i], " ") != strings.Join(want[i], " ") {
			t.Errorf("call %d = %v, want %v", i, got[i], want[i])
		}
	}
}

// mintFingerprint mints a CA for a cube and returns it with its ledger
// fingerprint.
func mintFingerprint(t *testing.T, cube string, seed byte) (certPEM []byte, fingerprint string) {
	t.Helper()
	material, _ := mustMint(t, cube, cube+".test", testNow(), seed)
	fingerprint, err := ca.Fingerprint(material.CertPEM)
	if err != nil {
		t.Fatalf("Fingerprint() error = %v", err)
	}
	return material.CertPEM, fingerprint
}

// TestMacOSInstall pins the argv and both failure paths. -d is absent
// from the expected add-trusted-cert call on purpose: it would target
// the admin store, and v0 trust never needs sudo.
func TestMacOSInstall(t *testing.T) {
	t.Parallel()

	addCert := []string{"security", "add-trusted-cert", "-r", "trustRoot", "-k", testKeychain, testCertPath}
	probe := []string{"security", "login-keychain"}

	cases := []struct {
		name      string
		respond   func(name string, args []string) ([]byte, []byte, error)
		wantCalls [][]string
		wantErr   bool
	}{
		{
			name:      "probe then add",
			respond:   keychainAnswer(nil),
			wantCalls: [][]string{probe, addCert},
		},
		{
			name:      "security is not installed",
			respond:   func(string, []string) ([]byte, []byte, error) { return nil, nil, notFound("security") },
			wantCalls: [][]string{probe},
			wantErr:   true,
		},
		{
			name: "add-trusted-cert fails",
			respond: keychainAnswer(func(string, []string) ([]byte, []byte, error) {
				return nil, []byte("SecTrustSettingsSetTrustSettings: The authorization was denied"), errors.New("exit status 1")
			}),
			wantCalls: [][]string{probe, addCert},
			wantErr:   true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runner := &fakeRunner{respond: tc.respond}

			err := ca.NewMacOSStore(runner.run).Install(t.Context(), testCube, testCertPath)
			if tc.wantErr {
				_ = assertCode(t, err, ca.CodeTrustStore)
			} else if err != nil {
				t.Fatalf("Install() error = %v", err)
			}
			assertCalls(t, runner.calls, tc.wantCalls)
		})
	}
}

// TestMacOSRemoveSelectsByFingerprint: two certificates share the marker
// CN, so only the ledger fingerprint distinguishes them — and the
// SHA-256 the driver passes to delete-certificate is the selected one's.
func TestMacOSRemoveSelectsByFingerprint(t *testing.T) {
	t.Parallel()
	otherPEM, otherFingerprint := mintFingerprint(t, testCube, 1)
	wantedPEM, wantedFingerprint := mintFingerprint(t, testCube, 2)
	deleted := false
	runner := &fakeRunner{}
	runner.respond = keychainAnswer(func(_ string, args []string) ([]byte, []byte, error) {
		if args[0] != "find-certificate" {
			deleted = true
			return nil, nil, nil
		}
		if deleted {
			return nil, nil, nil
		}
		out := append(findOutput(otherFingerprint, otherPEM), findOutput(wantedFingerprint, wantedPEM)...)
		return out, nil, nil
	})

	outcome, err := ca.NewMacOSStore(runner.run).Remove(t.Context(), ca.RemoveRequest{
		Cube: testCube, Fingerprint: wantedFingerprint, CertPath: testCertPath, CertPEM: wantedPEM,
	})
	if err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if !outcome.Found {
		t.Error("Found = false, want true")
	}
	assertCalls(t, runner.calls, [][]string{
		{"security", "login-keychain"},
		{"security", "find-certificate", "-a", "-p", "-Z", "-c", ca.CommonName(testCube), testKeychain},
		{"security", "delete-certificate", "-Z", strings.ToUpper(wantedFingerprint), "-t", testKeychain},
		{"security", "find-certificate", "-a", "-p", "-Z", "-c", ca.CommonName(testCube), testKeychain},
	})
}

// TestMacOSRemoveRefusesForeignCertificate: the ledger fingerprint
// matches, but the certificate behind it is another cube's CA. Both
// halves of the identity must hold, and nothing is deleted.
func TestMacOSRemoveRefusesForeignCertificate(t *testing.T) {
	t.Parallel()
	foreignPEM, foreignFingerprint := mintFingerprint(t, "other", 1)
	runner := &fakeRunner{}
	runner.respond = keychainAnswer(func(_ string, args []string) ([]byte, []byte, error) {
		if args[0] != "find-certificate" {
			t.Errorf("unexpected call %v — a refused removal must not mutate the keychain", args)
		}
		return findOutput(foreignFingerprint, foreignPEM), nil, nil
	})

	_, err := ca.NewMacOSStore(runner.run).Remove(t.Context(), ca.RemoveRequest{
		Cube: testCube, Fingerprint: foreignFingerprint, CertPath: testCertPath, CertPEM: foreignPEM,
	})
	_ = assertCode(t, err, ca.CodeTrustStore)
	if len(runner.calls) != 2 {
		t.Errorf("calls = %v, want the keychain lookup and the search only", runner.calls)
	}
}

// TestMacOSRemoveStaleEntry: find-certificate -a reports a miss as an
// empty result on a zero exit. That is the stale-ledger case, not a
// failure, and nothing is deleted.
func TestMacOSRemoveStaleEntry(t *testing.T) {
	t.Parallel()
	runner := &fakeRunner{respond: keychainAnswer(nil)}

	outcome, err := ca.NewMacOSStore(runner.run).Remove(t.Context(), ca.RemoveRequest{
		Cube: testCube, Fingerprint: "fce7a0ea", CertPath: testCertPath,
	})
	if err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if outcome.Found {
		t.Error("Found = true, want false for a certificate the keychain does not hold")
	}
	if len(runner.calls) != 2 {
		t.Errorf("calls = %v, want the keychain lookup and the search only", runner.calls)
	}
}

// TestMacOSRemoveConfirmsDeletion guards the sharpest macOS trap:
// delete-certificate exits 0 whether or not it deleted anything, so
// absence is established by looking again.
func TestMacOSRemoveConfirmsDeletion(t *testing.T) {
	t.Parallel()
	certPEM, fingerprint := mintFingerprint(t, testCube, 1)
	runner := &fakeRunner{}
	runner.respond = keychainAnswer(func(_ string, args []string) ([]byte, []byte, error) {
		if args[0] == "find-certificate" {
			return findOutput(fingerprint, certPEM), nil, nil
		}
		// delete-certificate lies: rc 0, certificate still there.
		return nil, nil, nil
	})

	_, err := ca.NewMacOSStore(runner.run).Remove(t.Context(), ca.RemoveRequest{
		Cube: testCube, Fingerprint: fingerprint, CertPath: testCertPath, CertPEM: certPEM,
	})
	_ = assertCode(t, err, ca.CodeTrustStore)
	if len(runner.calls) != 4 {
		t.Errorf("calls = %v, want the delete to be confirmed by a second search", runner.calls)
	}
}
