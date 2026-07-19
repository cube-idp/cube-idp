package flux

import (
	_ "embed"
	"testing"

	"github.com/cube-idp/cube-idp/internal/engine"
	"github.com/cube-idp/cube-idp/internal/engine/contract"
)

//go:embed testdata/crds.yaml
var crdsYAML []byte

func TestFluxContract(t *testing.T) {
	contract.Run(t, contract.Impl{
		Name: "flux",
		New:  func() engine.Engine { return New() },
		CRDs: func() ([]byte, error) { return crdsYAML, nil },
	})
}
