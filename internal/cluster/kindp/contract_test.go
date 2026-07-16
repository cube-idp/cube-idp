package kindp_test

import (
	"testing"

	"github.com/cube-idp/cube-idp/internal/cluster/contracttest"
	"github.com/cube-idp/cube-idp/internal/cluster/kindp"
	"github.com/cube-idp/cube-idp/internal/config"
)

func TestKindProviderContract(t *testing.T) {
	gw := config.GatewaySpec{Pack: "traefik", Host: "cube-idp.localtest.me", Port: 18443} // non-default port: avoid colliding with a dev cluster
	contracttest.Run(t, kindp.New(gw), config.ClusterSpec{Provider: "kind", KubernetesVersion: "v1.33.1"})
}
