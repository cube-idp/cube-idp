package k3dp_test

import (
	"testing"

	"github.com/cube-idp/cube-idp/internal/cluster/contracttest"
	"github.com/cube-idp/cube-idp/internal/cluster/k3dp"
	"github.com/cube-idp/cube-idp/internal/config"
)

func TestK3dProviderContract(t *testing.T) {
	gw := config.GatewaySpec{Pack: "traefik", Host: "cube-idp.localtest.me", Port: 28443} // non-default port: avoid colliding with a dev cluster or kindp's contract port
	contracttest.Run(t, k3dp.New(gw), config.ClusterSpec{Provider: "k3d", KubernetesVersion: "v1.33.1"})
}
