package cli

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/cube-idp/cube-idp/internal/ca"
	"github.com/cube-idp/cube-idp/internal/cubeerr"
)

const testMacKeychain = "/Users/tester/Library/Keychains/login.keychain-db"

// fakeTrustRunner is the trust-tool seam at the edge: it records argv and
// answers from a canned function, so `trust install` and `trust remove`
// are exercised end to end without touching a real trust store.
type fakeTrustRunner struct {
	calls   [][]string
	respond func(name string, args []string) (stdout, stderr []byte, err error)
}

func (f *fakeTrustRunner) run(_ context.Context, name string, args ...string) ([]byte, []byte, error) {
	f.calls = append(f.calls, append([]string{name}, args...))
	if f.respond == nil {
		return nil, nil, nil
	}
	return f.respond(name, args)
}

// answerKeychain replies to `security login-keychain` as the real tool
// does; every other call succeeds silently, which is what an install
// looks like and what a remove against an empty keychain looks like.
func answerKeychain(name string, args []string) ([]byte, []byte, error) {
	if name == "security" && len(args) == 1 && args[0] == "login-keychain" {
		return []byte("    \"" + testMacKeychain + "\"\n"), nil, nil
	}
	return nil, nil, nil
}

// execTrustStore runs the trust subtree against an injected home, OS name
// and trust-tool runner. It is a sibling of execTrust rather than a change
// to it: the list verb's tests need none of these seams.
func execTrustStore(t *testing.T, home, goos string, runner *fakeTrustRunner, args ...string) (
	code int, stdout, stderr string) {
	t.Helper()
	deps := trustDeps{
		homeDir: func() (string, error) { return home, nil },
		now:     func() time.Time { return time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC) },
		goos:    goos,
		run:     runner.run,
	}
	var out, errBuf bytes.Buffer
	code = execute(t.Context(), newTrustCmd(deps), args, &out, &errBuf)
	return code, out.String(), errBuf.String()
}

// seedArtifact mints a CA and emits it exactly where bootstrap would, so
// the verbs read a real certificate rather than a fixture that only looks
// like one.
func seedArtifact(t *testing.T, home, cube string) (certPath, fingerprint string) {
	t.Helper()
	material, _, err := ca.Mint(ca.MintRequest{
		CubeName: cube, Domain: cube + ".test", Now: time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC), Rand: rand.Reader,
	})
	if err != nil {
		t.Fatalf("Mint() error = %v", err)
	}
	certPath = ca.CertPath(trustRoot(home), cube)
	if _, err := syncCertFile(certPath, material.CertPEM); err != nil {
		t.Fatalf("syncCertFile() error = %v", err)
	}
	if fingerprint, err = ca.Fingerprint(material.CertPEM); err != nil {
		t.Fatalf("Fingerprint() error = %v", err)
	}
	return certPath, fingerprint
}

// readLedger reads back what a verb recorded.
func readLedger(t *testing.T, home string) ca.Ledger {
	t.Helper()
	ledger, err := loadLedger(ca.LedgerPath(trustRoot(home)))
	if err != nil {
		t.Fatalf("loadLedger() error = %v", err)
	}
	return ledger
}

// assertTrustCode asserts a rendered failure by code, and that the
// remediation named the way out.
func assertTrustCode(t *testing.T, code int, stderr string, wantRemedy string) {
	t.Helper()
	if code != 1 {
		t.Fatalf("exit = %d, want 1; stderr: %s", code, stderr)
	}
	if !strings.Contains(stderr, string(ca.CodeTrustStore)) {
		t.Errorf("stderr missing %s:\n%s", ca.CodeTrustStore, stderr)
	}
	if !strings.Contains(stderr, wantRemedy) {
		t.Errorf("stderr missing the remedy %q:\n%s", wantRemedy, stderr)
	}
}

// TestTrustInstall: the verb consumes the emitted artifact, hands it to
// the OS tool, and records the installation under the fingerprint it
// derived — not one it was told.
func TestTrustInstall(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	certPath, fingerprint := seedArtifact(t, home, "dev")
	runner := &fakeTrustRunner{respond: answerKeychain}

	code, stdout, stderr := execTrustStore(t, home, "darwin", runner, "install", "dev")
	if code != 0 {
		t.Fatalf("exit = %d, want 0; stderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, certPath) {
		t.Errorf("stdout = %q, want it to name %s", stdout, certPath)
	}
	assertArgv(t, runner.calls, [][]string{
		{"security", "login-keychain"},
		{"security", "add-trusted-cert", "-r", "trustRoot", "-k", testMacKeychain, certPath},
	})
	want := ca.Entry{Cube: "dev", Fingerprint: fingerprint, Store: "macos-login", Date: "2026-08-28"}
	if got := readLedger(t, home).Entries; len(got) != 1 || got[0] != want {
		t.Errorf("ledger = %+v, want exactly %+v", got, want)
	}
}

// TestTrustInstallTwice: re-installing replaces the cube's row instead of
// appending beside it — the ledger holds at most one entry per cube.
func TestTrustInstallTwice(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	seedArtifact(t, home, "dev")
	runner := &fakeTrustRunner{respond: answerKeychain}

	for range 2 {
		if code, _, stderr := execTrustStore(t, home, "darwin", runner, "install", "dev"); code != 0 {
			t.Fatalf("exit = %d, want 0; stderr: %s", code, stderr)
		}
	}
	if got := readLedger(t, home).Entries; len(got) != 1 {
		t.Errorf("ledger = %+v, want exactly one entry", got)
	}
}

// TestTrustInstallMissingArtifact: the verb never mints and never reads
// the cluster — with no emitted certificate it says so and points at
// bootstrap, without invoking any OS tool.
func TestTrustInstallMissingArtifact(t *testing.T) {
	t.Parallel()
	runner := &fakeTrustRunner{}

	code, _, stderr := execTrustStore(t, t.TempDir(), "darwin", runner, "install", "dev")
	assertTrustCode(t, code, stderr, "bootstrap")
	if len(runner.calls) != 0 {
		t.Errorf("calls = %v, want none", runner.calls)
	}
}

// TestTrustInstallUnsupportedOS: an OS with no driver is a coded failure
// naming the artifact, never a silent no-op.
func TestTrustInstallUnsupportedOS(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	runner := &fakeTrustRunner{}

	code, _, stderr := execTrustStore(t, home, "windows", runner, "install", "dev")
	assertTrustCode(t, code, stderr, ca.CertPath(trustRoot(home), "dev"))
	if !strings.Contains(stderr, "windows") {
		t.Errorf("stderr does not name the OS:\n%s", stderr)
	}
	if len(runner.calls) != 0 {
		t.Errorf("calls = %v, want none", runner.calls)
	}
}

// TestReadCertFileUnreadable: only an ABSENT certificate is (nil, nil).
// Anything else the edge cannot read is an error, never a silent nil that
// a store would read as "nothing to verify".
func TestReadCertFileUnreadable(t *testing.T) {
	t.Parallel()
	path := ca.CertPath(trustRoot(t.TempDir()), "dev")
	if err := os.MkdirAll(path, artifactDirMode); err != nil {
		t.Fatal(err)
	}

	certPEM, err := readCertFile("dev", path)
	if err == nil {
		t.Fatalf("readCertFile() = (%q, nil), want an error", certPEM)
	}
	var coded *cubeerr.Coded
	if !errors.As(err, &coded) || coded.Code != ca.CodeTrustStore {
		t.Errorf("readCertFile() error = %v, want a %s cubeerr.Coded", err, ca.CodeTrustStore)
	}
}

// TestDefaultTrustDepsAreComplete guards the composition itself: a nil
// seam would panic only on the OS that selects it, which no gate runs.
func TestDefaultTrustDepsAreComplete(t *testing.T) {
	t.Parallel()

	deps := defaultTrustDeps()
	if deps.homeDir == nil || deps.now == nil || deps.run == nil || deps.goos == "" {
		t.Errorf("defaultTrustDeps() = %+v, want every seam populated", deps)
	}
}

// assertArgv asserts the exact commands the edge ran.
func assertArgv(t *testing.T, got, want [][]string) {
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
