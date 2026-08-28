package cli

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"

	"github.com/cube-idp/cube-idp/internal/ca"
)

// dateFormat is the ledger's installation date: a plain calendar day,
// formatted from the injected clock so the domain never reads one.
const dateFormat = "2006-01-02"

// defaultRunner is the production ca.Runner. os/exec lives here, at the
// edge, which is what lets every trust driver run in the hermetic gate
// against a fake. Output is buffered rather than streamed: the drivers
// parse stdout, and stderr is the cause a coded error carries.
func defaultRunner(ctx context.Context, name string, args ...string) (stdout, stderr []byte, err error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err = cmd.Run()
	return out.Bytes(), errOut.Bytes(), err
}

// trustStore selects the user-scope trust store for the running OS,
// switching on a string exactly as the provisioner and engine factories
// do. An OS with no driver is a coded error naming the emitted
// certificate, never a silent no-op.
func trustStore(deps trustDeps, certPath string) (ca.Store, error) {
	switch deps.goos {
	case "darwin":
		return ca.NewMacOSStore(deps.run), nil
	case "linux":
		return ca.NewP11KitStore(deps.run), nil
	default:
		return nil, ca.NewUnsupportedStoreError(deps.goos, certPath)
	}
}

// readCertFile reads an emitted CA certificate. An absent file is
// reported as (nil, nil), because the two verbs answer it differently:
// `install` has nothing to install, while `remove` hands the nil on to
// the store, which knows whether it can identify the certificate
// without the local copy. Any other read failure is CUBE-CA-004 — the
// caller has a cube name to name in the error, so the coded constructor
// is raised here rather than left to escape as an uncoded error.
func readCertFile(cube, path string) ([]byte, error) {
	certPEM, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, nil
	case err != nil:
		return nil, ca.NewArtifactReadError(cube, path, err)
	}
	return certPEM, nil
}
