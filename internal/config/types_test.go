package config

import "testing"

func TestGatewaySpecPackRef(t *testing.T) {
	cases := []struct {
		name string
		gw   GatewaySpec
		want string
	}{
		{"explicit ref wins", GatewaySpec{Pack: "traefik", Ref: "/abs/repo/packs/traefik"}, "/abs/repo/packs/traefik"},
		{"falls back to pack name", GatewaySpec{Pack: "traefik"}, "packs/traefik"},
		{"empty ref falls back", GatewaySpec{Pack: "nginx", Ref: ""}, "packs/nginx"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.gw.PackRef(); got != tc.want {
				t.Fatalf("PackRef() = %q, want %q", got, tc.want)
			}
		})
	}
}
