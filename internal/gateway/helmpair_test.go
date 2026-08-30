package gateway_test

import (
	"reflect"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/cube-idp/cube-idp/internal/gateway"
)

// TestHelmPairObjects is the golden for the hand-built CR pair. The edge
// dogfood test proves the pair equals what the pack contract renders; this
// one states what the pair is, so a change shows up as a diff here rather
// than only as an equality failure in another package.
func TestHelmPairObjects(t *testing.T) {
	got := gateway.HelmPairObjects()
	if len(got) != 2 {
		t.Fatalf("HelmPairObjects() = %d objects, want the source + release pair", len(got))
	}
	want := []map[string]any{
		{
			"apiVersion": "source.toolkit.fluxcd.io/v1",
			"kind":       "OCIRepository",
			"metadata":   map[string]any{"name": "traefik-gateway"},
			"spec": map[string]any{
				"url":      "oci://ghcr.io/traefik/helm/traefik",
				"interval": "10m",
				"layerSelector": map[string]any{
					"mediaType": "application/vnd.cncf.helm.chart.content.v1.tar+gzip",
					"operation": "copy",
				},
				"ref": map[string]any{
					"tag":    "41.3.0",
					"digest": "sha256:dcae2d586d7fbda6a08150eaeeca4132e9dd042d8a4d16ada287e8c40f6ff17a",
				},
			},
		},
		{
			"apiVersion": "helm.toolkit.fluxcd.io/v2",
			"kind":       "HelmRelease",
			"metadata":   map[string]any{"name": "traefik-gateway"},
			"spec": map[string]any{
				"interval":        "10m",
				"releaseName":     "traefik-gateway",
				"targetNamespace": "gateway-system",
				"install":         map[string]any{"createNamespace": true},
				"chartRef":        map[string]any{"kind": "OCIRepository", "name": "traefik-gateway"},
				"values": map[string]any{
					"providers":        map[string]any{"kubernetesGateway": map[string]any{"enabled": true}},
					"gateway":          map[string]any{"enabled": false},
					"nodeSelector":     map[string]any{"ingress-ready": "true"},
					"fullnameOverride": "traefik-gateway",
					"ports": map[string]any{
						"web":       map[string]any{"hostPort": float64(80)},
						"websecure": map[string]any{"hostPort": float64(443)},
					},
				},
			},
		},
	}
	for i := range want {
		if !reflect.DeepEqual(got[i].Object, want[i]) {
			t.Errorf("object %d:\n got %#v\nwant %#v", i, got[i].Object, want[i])
		}
		if got[i].GetNamespace() != "" {
			t.Errorf("object %d carries metadata.namespace %q; the pair is namespace-less until the edge stamps it",
				i, got[i].GetNamespace())
		}
	}
}

// TestHelmPairHostPortsAreFloat64 names the trap directly, so a future
// change to an untyped int regresses with a message that explains itself
// instead of a deep-equality dump in which the two values look identical.
// pack.Render round-trips spec.values through JSON, and json.Unmarshal
// produces float64 for every number.
func TestHelmPairHostPortsAreFloat64(t *testing.T) {
	release := gateway.HelmPairObjects()[1]
	for _, port := range []string{"web", "websecure"} {
		raw, found, err := unstructured.NestedFieldNoCopy(release.Object,
			"spec", "values", "ports", port, "hostPort")
		if err != nil || !found {
			t.Fatalf("ports.%s.hostPort: found=%v, err=%v", port, found, err)
		}
		if _, ok := raw.(float64); !ok {
			t.Errorf("ports.%s.hostPort is %T, want float64 — pack.Render's values are JSON round-tripped", port, raw)
		}
	}
}
