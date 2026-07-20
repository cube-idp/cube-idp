package pack

import (
	"context"
	"fmt"

	"github.com/cube-idp/cube-idp/internal/config"
	"github.com/cube-idp/cube-idp/internal/diag"
)

// FetchRenderEngine fetches the engine pack at ref and renders it with the
// engine's values — this replaced the engine's own hardcoded install
// manifests, so the returned objects
// are what `up` SSAs, what the inventory records, and (selfManage) what the
// cube-engine artifact carries. ref is passed explicitly rather than
// derived from spec so offline mode can hand in the bundle-resolved dir.
func FetchRenderEngine(ctx context.Context, spec config.EngineSpec, gw config.GatewaySpec, ref, cacheDir string) (*Pack, *Rendered, error) {
	pk, err := Fetch(ctx, ref, cacheDir)
	if err != nil {
		return nil, nil, err
	}
	if err := VerifyEnginePackRef(pk, spec); err != nil {
		return nil, nil, err
	}
	rendered, err := pk.RenderWith(spec.Values, "", gw)
	if err != nil {
		return nil, nil, err
	}
	return pk, rendered, nil
}

// VerifyEnginePackRef is the engine twin of up's F11 gateway check
// (CUBE-0013): the fetched pack's declared pack.cue name must be exactly
// cube-engine-<engine.type>, so pointing the argocd engine at the flux
// pack (or any ordinary pack) fails before any cluster mutation.
func VerifyEnginePackRef(p *Pack, spec config.EngineSpec) error {
	if p.Name == spec.PackName() {
		return nil
	}
	return diag.New(diag.CodeEnginePackMismatch,
		fmt.Sprintf("spec.engine.ref resolves to the %q pack, but engine.type %q requires pack %q", p.Name, spec.Type, spec.PackName()),
		fmt.Sprintf("point spec.engine.ref at the %s pack or remove it to use the published default", spec.PackName()))
}
