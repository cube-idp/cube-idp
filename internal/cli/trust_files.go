package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/cube-idp/cube-idp/internal/ca"
)

// File modes for the operator-artifact tree. The tree records which CAs
// a user installed into their own trust stores; nothing in it is meant
// for other users, so the directories are 0o700 — the same mode at every
// level, because MkdirAll never tightens a directory that already
// exists. The certificate itself is the public half and is written
// 0o644 inside that private tree; the ledger is 0o600.
const (
	artifactDirMode  fs.FileMode = 0o700
	artifactCertMode fs.FileMode = 0o644
	artifactFileMode fs.FileMode = 0o600
)

// trustRoot returns the operator-artifact root under a home directory.
func trustRoot(home string) string {
	return filepath.Join(home, ca.DirName)
}

// artifactRoot resolves the artifact root from the injected home
// lookup. A home-less environment is an error, never a silent
// CWD-relative fallback — defaultKubeconfigPath's rule
// (internal/cluster/init.go), which matters more here: a mistaken root
// would write trust material records into whatever directory the user
// happened to be standing in.
func artifactRoot(homeDir func() (string, error)) (string, error) {
	home, err := homeDir()
	if err != nil {
		return "", fmt.Errorf("determine the cube-idp artifact directory: %w", err)
	}
	return trustRoot(home), nil
}

// loadLedger reads and parses the ledger at path. An absent file is the
// empty ledger — a machine that never ran `trust install` has nothing
// recorded. Every other read failure is CUBE-CA-003: an unreadable
// ledger is never quietly treated as an empty one.
func loadLedger(path string) (ca.Ledger, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return ca.Ledger{}, nil
		}
		return ca.Ledger{}, ca.NewLedgerUnreadableError(path, err)
	}
	return ca.ParseLedger(raw)
}

// saveLedger renders and writes the ledger to path atomically. The write
// failure is returned uncoded: the verb that writes is what knows which
// operation failed, and it wraps accordingly.
func saveLedger(path string, ledger ca.Ledger) error {
	data, err := ledger.Marshal()
	if err != nil {
		return err
	}
	return writeArtifact(path, data, artifactFileMode)
}

// syncCertFile idempotently synchronizes a cube's CA certificate
// artifact: it creates the file when absent, corrects it when the bytes
// diverge, and reports changed=false when it already matches. Emission
// is never conditioned on the CA having been minted — a reused CA must
// still restore a file the user deleted (docs/domains/ca.md).
//
// This is the symbol the bootstrap edge calls once its CA wiring lands
// (M11-C6, issue #187), with the path from ca.CertPath(root, cube).
func syncCertFile(path string, certPEM []byte) (changed bool, err error) {
	existing, err := os.ReadFile(path)
	switch {
	case err == nil && bytes.Equal(existing, certPEM):
		return false, nil
	case err != nil && !errors.Is(err, fs.ErrNotExist):
		return false, fmt.Errorf("read CA certificate %s: %w", path, err)
	}
	if err := writeArtifact(path, certPEM, artifactCertMode); err != nil {
		return false, err
	}
	return true, nil
}

// writeArtifact writes an artifact file atomically — temp file in the
// target directory, then rename — so a crash mid-write can never
// truncate what is already there. The precedent is writeKubeconfig
// (internal/cluster/init.go).
func writeArtifact(path string, data []byte, mode fs.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, artifactDirMode); err != nil {
		return fmt.Errorf("create artifact dir %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".cube-idp-*")
	if err != nil {
		return fmt.Errorf("temp file for %s: %w", path, err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }() // no-op once renamed
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := os.Chmod(tmp.Name(), mode); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
