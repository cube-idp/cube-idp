package argocd

import (
	_ "embed"
	"testing"

	"github.com/cube-idp/cube-idp/internal/engine"
	"github.com/cube-idp/cube-idp/internal/engine/contract"
)

//go:embed testdata/crds.yaml
var crdsYAML []byte

func TestArgoCDContract(t *testing.T) {
	contract.Run(t, contract.Impl{
		Name: "argocd",
		New:  func() engine.Engine { return New() },
		CRDs: func() ([]byte, error) { return crdsYAML, nil },
	})
}
