package argocd

import (
	"testing"

	"github.com/cube-idp/cube-idp/internal/engine"
	"github.com/cube-idp/cube-idp/internal/engine/contract"
)

func TestArgoCDContract(t *testing.T) {
	contract.Run(t, contract.Impl{
		Name: "argocd",
		New:  func() engine.Engine { return New() },
	})
}
