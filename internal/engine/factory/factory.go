// Package factory constructs a concrete engine.Engine by name. It lives in
// its own package (rather than engine.New) so the up orchestrator and
// future commands share one construction path without engine importing its
// own implementations: engine defines the seam, flux implements it, and
// factory is the only package that needs to know about both.
package factory

import (
	"fmt"

	"github.com/cube-idp/cube-idp/internal/config"
	"github.com/cube-idp/cube-idp/internal/diag"
	"github.com/cube-idp/cube-idp/internal/engine"
	"github.com/cube-idp/cube-idp/internal/engine/argocd"
	"github.com/cube-idp/cube-idp/internal/engine/flux"
)

// New builds the Engine named by spec.Type, carrying spec.Tuning into the
// engine so InstallManifests returns the tuned objects (GT1, U3). "flux"
// and "argocd" (D2) both ship; anything else is unknown (CUBE-3001).
func New(spec config.EngineSpec) (engine.Engine, error) {
	switch spec.Type {
	case "flux":
		return flux.NewTuned(spec.Tuning), nil
	case "argocd":
		return argocd.NewTuned(spec.Tuning), nil
	default:
		return nil, diag.New(diag.CodeEngineTypeUnknown, fmt.Sprintf("unknown engine type %q", spec.Type),
			"use engine.type: flux or argocd")
	}
}
