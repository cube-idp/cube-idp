package cli

import (
	"strings"
	"testing"

	"github.com/cube-idp/cube-idp/internal/ca"
)

// TestTrustRemoveStaleEntry: a recorded certificate the store no longer
// holds is not a failure. The entry is dropped and the verb says why.
// No artifact is emitted here on purpose — macOS identifies the
// certificate from the keychain, so removal never needs the local file.
func TestTrustRemoveStaleEntry(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	writeLedger(t, home, twoEntryLedger)
	// answerKeychain returns empty find-certificate output: nothing found.
	runner := &fakeTrustRunner{respond: answerKeychain}

	code, stdout, stderr := execTrustStore(t, home, "darwin", runner, "remove", "dev")
	if code != 0 {
		t.Fatalf("exit = %d, want 0; stderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "stale ledger entry") {
		t.Errorf("stdout = %q, want it to report the stale entry", stdout)
	}
	if _, found := readLedger(t, home).Find("dev"); found {
		t.Error("the stale entry survived the removal")
	}
}

// TestTrustRemoveUnrecorded: removing a cube the ledger never recorded is
// the same idempotent success as removing a stale one.
func TestTrustRemoveUnrecorded(t *testing.T) {
	t.Parallel()
	runner := &fakeTrustRunner{}

	code, stdout, stderr := execTrustStore(t, t.TempDir(), "darwin", runner, "remove", "dev")
	if code != 0 {
		t.Fatalf("exit = %d, want 0; stderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "no record") {
		t.Errorf("stdout = %q, want it to report that nothing was recorded", stdout)
	}
	if len(runner.calls) != 0 {
		t.Errorf("calls = %v, want none", runner.calls)
	}
}

// TestTrustRemoveOnLinux covers the wiring the two stores do not share:
// p11-kit can only verify the local artifact, so the edge must read it
// off disk and hand the bytes down beside the path the tool takes.
func TestTrustRemoveOnLinux(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	certPath, fingerprint := seedArtifact(t, home, "dev")
	if err := saveLedger(ca.LedgerPath(trustRoot(home)), ca.Ledger{Entries: []ca.Entry{
		{Cube: "dev", Fingerprint: fingerprint, Store: "p11-kit", Date: "2026-08-28"},
	}}); err != nil {
		t.Fatalf("saveLedger() error = %v", err)
	}
	runner := &fakeTrustRunner{}

	code, _, stderr := execTrustStore(t, home, "linux", runner, "remove", "dev")
	if code != 0 {
		t.Fatalf("exit = %d, want 0; stderr: %s", code, stderr)
	}
	assertArgv(t, runner.calls, [][]string{{"trust", "anchor", "--remove", certPath}})
	if _, found := readLedger(t, home).Find("dev"); found {
		t.Error("the entry survived a successful removal")
	}
}

// TestTrustRemoveStoreMismatch: the ledger's store field exists to make
// this decidable. Searching a store the certificate was never put into is
// refused before any tool runs.
func TestTrustRemoveStoreMismatch(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	writeLedger(t, home, twoEntryLedger)
	runner := &fakeTrustRunner{}

	code, _, stderr := execTrustStore(t, home, "darwin", runner, "remove", "staging")
	assertTrustCode(t, code, stderr, "trust remove staging")
	if len(runner.calls) != 0 {
		t.Errorf("calls = %v, want none", runner.calls)
	}
	if _, found := readLedger(t, home).Find("staging"); !found {
		t.Error("a refused removal dropped the ledger entry")
	}
}
