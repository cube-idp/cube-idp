package up

import (
	"testing"

	"github.com/rafpe/cube-idp/internal/config"
)

func TestGatewayPackRefPrefersExplicitRef(t *testing.T) {
	got := gatewayPackRef(config.GatewaySpec{Pack: "traefik", Ref: "/abs/repo/packs/traefik"})
	if want := "/abs/repo/packs/traefik"; got != want {
		t.Fatalf("gatewayPackRef() = %q, want %q", got, want)
	}
}

func TestGatewayPackRefFallsBackToPackName(t *testing.T) {
	got := gatewayPackRef(config.GatewaySpec{Pack: "traefik"})
	if want := "packs/traefik"; got != want {
		t.Fatalf("gatewayPackRef() = %q, want %q", got, want)
	}
}

func TestGatewayPackRefEmptyRefFallsBack(t *testing.T) {
	got := gatewayPackRef(config.GatewaySpec{Pack: "nginx", Ref: ""})
	if want := "packs/nginx"; got != want {
		t.Fatalf("gatewayPackRef() = %q, want %q", got, want)
	}
}
