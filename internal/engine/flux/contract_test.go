package flux

import (
	"testing"

	"github.com/cube-idp/cube-idp/internal/engine"
	"github.com/cube-idp/cube-idp/internal/engine/contract"
)

func TestFluxContract(t *testing.T) {
	contract.Run(t, contract.Impl{
		Name: "flux",
		New:  func() engine.Engine { return New() },
	})
}
