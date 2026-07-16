// Package factory constructs a concrete engine.Engine by name. It lives in
// its own package (rather than engine.New) so the up orchestrator and
// future commands share one construction path without engine importing its
// own implementations: engine defines the seam, flux implements it, and
// factory is the only package that needs to know about both.
package factory

import (
	"fmt"

	"github.com/cube-idp/cube-idp/internal/diag"
	"github.com/cube-idp/cube-idp/internal/engine"
	"github.com/cube-idp/cube-idp/internal/engine/argocd"
	"github.com/cube-idp/cube-idp/internal/engine/flux"
)

// New builds the Engine named by typ. "flux" and "argocd" (D2) both ship;
// anything else is unknown (CUBE-3001).
func New(typ string) (engine.Engine, error) {
	switch typ {
	case "flux":
		return flux.New(), nil
	case "argocd":
		return argocd.New(), nil
	default:
		return nil, diag.New(diag.CodeEngineTypeUnknown, fmt.Sprintf("unknown engine type %q", typ),
			"use engine.type: flux or argocd")
	}
}
