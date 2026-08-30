package ca

import "path/filepath"

// The operator-artifact layout under the user's home directory
// (docs/domains/ca.md, trust distribution). The names are user-visible
// and effectively frozen once a machine has files on disk.
const (
	// DirName is the artifact directory: ~/.cube-idp.
	DirName = ".cube-idp"
	// LedgerFileName is the trust ledger, one file for every cube and
	// every store — it spans both by nature.
	LedgerFileName = "trust.yaml"
	// CertFileName is a cube's CA certificate, the only exported
	// artifact (the private key never leaves the in-cluster Secret).
	CertFileName = "ca.crt"
)

// LedgerPath returns the trust ledger's path under an artifact root:
// <root>/trust.yaml. Derivation only — resolving the root from the
// user's home and touching the file stay at the CLI edge.
func LedgerPath(root string) string {
	return filepath.Join(root, LedgerFileName)
}

// CertPath returns a cube's CA certificate path under an artifact root:
// <root>/<cube>/ca.crt.
func CertPath(root, cube string) string {
	return filepath.Join(root, cube, CertFileName)
}
