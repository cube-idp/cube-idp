package flux

import (
	"testing"

	"github.com/rafpe/cube-idp/internal/engine"
	"github.com/rafpe/cube-idp/internal/engine/contract"
)

func TestFluxContract(t *testing.T) {
	contract.Run(t, contract.Impl{
		Name: "flux",
		New:  func() engine.Engine { return New() },
	})
}
