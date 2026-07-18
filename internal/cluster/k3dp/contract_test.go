package k3dp_test

import (
	"context"
	"testing"

	"github.com/cube-idp/cube-idp/internal/cluster/contracttest"
	"github.com/cube-idp/cube-idp/internal/cluster/k3dp"
	"github.com/cube-idp/cube-idp/internal/config"
	"github.com/cube-idp/cube-idp/internal/diag"
)

func TestK3dProviderContract(t *testing.T) {
	gw := config.GatewaySpec{Pack: "traefik", Host: "cube-idp.localtest.me", Port: 28443} // non-default port: avoid colliding with a dev cluster or kindp's contract port
	contracttest.Run(t, k3dp.New(gw), config.ClusterSpec{Provider: "k3d", KubernetesVersion: "v1.33.1"})
}

func TestRenderContract(t *testing.T) {
	base := config.ClusterSpec{Provider: "k3d", KubernetesVersion: "v1.33.1"}
	conflict := base
	conflict.ForProvider = map[string]any{"image": "rancher/k3s:v1.99.0-k3s1"}
	contracttest.RenderContract(t, base, conflict,
		func(s config.ClusterSpec) ([]byte, []diag.Finding, error) {
			return k3dp.RenderConfig(context.Background(), "contract", s, config.GatewaySpec{Pack: "traefik", Host: "h", Port: 8443}, k3dp.ZotMirror{})
		})
}
