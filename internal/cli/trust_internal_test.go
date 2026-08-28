package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cube-idp/cube-idp/internal/ca"
)

const twoEntryLedger = `entries:
- cube: dev
  fingerprint: fce7a0ea053961041be2f12aa126ab20b1c38dff1656451b32199bfda93e0702
  store: macos-login
  date: "2026-08-28"
- cube: staging
  fingerprint: 91416107a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c
  store: p11-kit
  date: "2026-08-27"
`

// execTrust runs the trust subtree against an injected home directory, so
// no test can read or write the real $HOME. The subtree carries the
// root's SilenceUsage/SilenceErrors, so what these tests see on stderr is
// what production prints.
func execTrust(t *testing.T, home string, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	deps := trustDeps{
		homeDir: func() (string, error) { return home, nil },
		now:     func() time.Time { return time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC) },
		goos:    "darwin",
	}
	var out, errBuf bytes.Buffer
	code = execute(t.Context(), newTrustCmd(deps), args, &out, &errBuf)
	return code, out.String(), errBuf.String()
}

// writeLedger seeds the injected home with a ledger file.
func writeLedger(t *testing.T, home, content string) {
	t.Helper()
	root := trustRoot(home)
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ca.LedgerPath(root), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestTrustListNoLedger: a machine that never installed anything is a
// finding, not a failure — the message goes to stdout and the exit is 0.
func TestTrustListNoLedger(t *testing.T) {
	t.Parallel()

	code, stdout, stderr := execTrust(t, t.TempDir(), "list")
	if code != 0 {
		t.Fatalf("exit = %d, want 0; stderr: %s", code, stderr)
	}
	if want := "no CA certificates installed by cube-idp\n"; stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
}

// TestTrustListGolden is the byte-exact check of the rendered table.
func TestTrustListGolden(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	writeLedger(t, home, twoEntryLedger)

	code, stdout, stderr := execTrust(t, home, "list")
	if code != 0 {
		t.Fatalf("exit = %d, want 0; stderr: %s", code, stderr)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "trust-list.golden"))
	if err != nil {
		t.Fatal(err)
	}
	if stdout != string(want) {
		t.Errorf("trust list output differs from testdata/trust-list.golden:\ngot:\n%s\nwant:\n%s", stdout, want)
	}
}

// TestTrustListMalformedLedger: a hand-edited ledger fails loudly with
// the ledger code, and the remediation names the file to fix or delete.
func TestTrustListMalformedLedger(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	writeLedger(t, home, "entries:\n- cube: dev\n  issuer: someone\n")

	code, _, stderr := execTrust(t, home, "list")
	if code != 1 {
		t.Fatalf("exit = %d, want 1; stderr: %s", code, stderr)
	}
	for _, want := range []string{string(ca.CodeLedger), "trust.yaml"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr missing %q:\n%s", want, stderr)
		}
	}
}

// TestTrustListUnreadableLedger guards the natural wrong
// implementation: only an ABSENT file is an empty ledger. A ledger the
// edge cannot read is CUBE-CA-003, never silently reported as empty.
func TestTrustListUnreadableLedger(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	if err := os.MkdirAll(ca.LedgerPath(trustRoot(home)), 0o700); err != nil {
		t.Fatal(err)
	}

	code, stdout, stderr := execTrust(t, home, "list")
	if code != 1 {
		t.Fatalf("exit = %d, want 1; stdout: %s", code, stdout)
	}
	if !strings.Contains(stderr, string(ca.CodeLedger)) {
		t.Errorf("stderr missing %s:\n%s", ca.CodeLedger, stderr)
	}
}

// TestTrustListHomeUnresolvable: a home-less environment is an error,
// never a silent fallback to whatever directory the user stands in.
func TestTrustListHomeUnresolvable(t *testing.T) {
	t.Parallel()

	_, err := artifactRoot(func() (string, error) { return "", errors.New("no home") })
	if err == nil {
		t.Fatal("artifactRoot() with an unresolvable home = nil error, want an error")
	}
}

// TestSaveLedgerRoundTrip: what saveLedger writes, loadLedger reads back,
// and the file is private to the user.
func TestSaveLedgerRoundTrip(t *testing.T) {
	t.Parallel()
	path := ca.LedgerPath(trustRoot(t.TempDir()))
	ledger := ca.Ledger{Entries: []ca.Entry{
		{Cube: "staging", Fingerprint: "bb", Store: "p11-kit", Date: "2026-08-27"},
		{Cube: "dev", Fingerprint: "aa", Store: "macos-login", Date: "2026-08-28"},
	}}

	if err := saveLedger(path, ledger); err != nil {
		t.Fatalf("saveLedger() error = %v", err)
	}
	got, err := loadLedger(path)
	if err != nil {
		t.Fatalf("loadLedger() error = %v", err)
	}
	if len(got.Entries) != 2 || got.Entries[0].Cube != "dev" {
		t.Errorf("round trip = %+v, want the entries sorted by cube", got.Entries)
	}
	// Only the file mode is asserted: it is set with an explicit Chmod and
	// so is deterministic, whereas MkdirAll applies the caller's umask.
	assertMode(t, path, artifactFileMode)
}

// TestSyncCertFile is the C6 handoff symbol's contract: create when
// absent, correct when divergent, and report no change when the bytes
// already match — emission is never conditioned on a mint.
func TestSyncCertFile(t *testing.T) {
	t.Parallel()
	const certPEM = "-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n"

	cases := []struct {
		name        string
		existing    string
		wantChanged bool
	}{
		{name: "absent file is created", wantChanged: true},
		{name: "divergent bytes are corrected", existing: "-----BEGIN CERTIFICATE-----\nstale\n", wantChanged: true},
		{name: "identical bytes are left alone", existing: certPEM},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// The per-cube parent directory is deliberately never
			// pre-created: syncCertFile owns the whole tree.
			path := ca.CertPath(trustRoot(t.TempDir()), "dev")
			var before os.FileInfo
			if tc.existing != "" {
				if err := os.MkdirAll(filepath.Dir(path), artifactDirMode); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(tc.existing), artifactCertMode); err != nil {
					t.Fatal(err)
				}
				var err error
				if before, err = os.Stat(path); err != nil {
					t.Fatal(err)
				}
			}

			changed, err := syncCertFile(path, []byte(certPEM))
			if err != nil {
				t.Fatalf("syncCertFile() error = %v", err)
			}
			if changed != tc.wantChanged {
				t.Errorf("changed = %v, want %v", changed, tc.wantChanged)
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read back: %v", err)
			}
			if string(got) != certPEM {
				t.Errorf("file = %q, want %q", got, certPEM)
			}
			// Matching bytes must leave the file itself alone, not
			// rewrite it and report no change: the atomic write renames a
			// new file over the old one, so identity is the sharp test.
			if before != nil {
				after, err := os.Stat(path)
				if err != nil {
					t.Fatal(err)
				}
				if same := os.SameFile(before, after); same == tc.wantChanged {
					t.Errorf("file replaced = %v, want %v", !same, tc.wantChanged)
				}
			}
		})
	}
}

// TestSyncCertFileUnreadable: an existing path the edge cannot read is
// an error, never quietly overwritten.
func TestSyncCertFileUnreadable(t *testing.T) {
	t.Parallel()
	path := ca.CertPath(trustRoot(t.TempDir()), "dev")
	if err := os.MkdirAll(path, artifactDirMode); err != nil {
		t.Fatal(err)
	}

	if changed, err := syncCertFile(path, []byte("cert")); err == nil {
		t.Fatalf("syncCertFile() = (%v, nil), want an error", changed)
	}
}

// assertMode asserts a path's permission bits, so the documented
// artifact modes are the ones actually written.
func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode() != want {
		t.Errorf("%s mode = %v, want %v", path, info.Mode(), want)
	}
}
