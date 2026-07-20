// Package cfgload dispatches the -f flag between a local cube.yaml and a
// remote ref (spec 2026-07-19 §7.1). It exists as its own package — not
// inside config.Load as the spec first sketched — because config must not
// import pack (pack imports config); the observable contract is identical:
// one dispatch point, every command inherits remote -f, zero flag changes.
//
// Dispatch: an existing local path always wins (this also disambiguates
// names like configs.d/cube.yaml that would otherwise parse as bare-git
// refs). A missing path that is remote-shaped (pack.IsRemoteRef) is
// fetched through the pack ref grammar — read-only: config.SaveValidated
// refuses remote-origin cubes (CUBE-0014). Anything else falls through to
// config.Load for the canonical CUBE-0001.
package cfgload

import (
	"context"
	"fmt"
	"os"

	"github.com/cube-idp/cube-idp/internal/config"
	"github.com/cube-idp/cube-idp/internal/diag"
	"github.com/cube-idp/cube-idp/internal/pack"
)

// Load resolves pathOrRef to a validated *config.Cube. Remote loads carry a
// remote Origin (ref + fetched pin) so callers can print the provenance
// line and relocate cube.lock to the working directory.
func Load(ctx context.Context, pathOrRef string) (*config.Cube, error) {
	if _, err := os.Stat(pathOrRef); err == nil {
		return config.Load(pathOrRef)
	}
	if !pack.IsRemoteRef(pathOrRef) {
		return config.Load(pathOrRef) // canonical CUBE-0001 for a missing local file
	}
	cacheDir, err := pack.DefaultCacheDir()
	if err != nil {
		return nil, err
	}
	raw, pin, err := pack.FetchFile(ctx, pathOrRef, cacheDir)
	if err != nil {
		return nil, diag.Wrap(err, diag.CodeConfigRemoteFetch,
			fmt.Sprintf("cannot fetch remote config %q", pathOrRef),
			"the -f ref must resolve to one readable cube.yaml; check the ref, your network, and credentials")
	}
	cube, err := config.LoadBytes(raw, pathOrRef)
	if err != nil {
		return nil, err
	}
	cube.MarkRemoteOrigin(pathOrRef, pin)
	return cube, nil
}
