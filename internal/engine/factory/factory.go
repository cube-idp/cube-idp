// Package factory constructs a concrete engine.Engine by name. It lives in
// its own package (rather than engine.New) so the up orchestrator and
// future commands share one construction path without engine importing its
// own implementations: engine defines the seam, flux implements it, and
// factory is the only package that needs to know about both.
package factory

import (
	"fmt"

	"github.com/rafpe/cube-idp/internal/diag"
	"github.com/rafpe/cube-idp/internal/engine"
	"github.com/rafpe/cube-idp/internal/engine/flux"
)

// New builds the Engine named by typ. "flux" is the only Phase 1 engine;
// "argocd" is a known, not-yet-shipped Phase 2 engine (CUBE-3002); anything
// else is unknown (CUBE-3001).
func New(typ string) (engine.Engine, error) {
	switch typ {
	case "flux":
		return flux.New(), nil
	case "argocd":
		return nil, diag.New("CUBE-3002", "the argocd engine ships in Phase 2 (D2)",
			"use engine.type: flux for now; argocd is available as a UI pack today")
	default:
		return nil, diag.New("CUBE-3001", fmt.Sprintf("unknown engine type %q", typ),
			"use engine.type: flux or argocd")
	}
}
