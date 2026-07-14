package pack

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/rafpe/cube-idp/internal/config"
)

// SecretRef locates a Kubernetes Secret a pack's expose block points at.
type SecretRef struct{ Namespace, Name string }

// Expose is the D11 contract: how a pack declares its endpoints and
// credentials — in data, never in Go. Parsed from pack.cue's optional
// expose: block; rendered by `up` into the pack's Pack record.
type Expose struct {
	URLs          []string
	AuthSecretRef *SecretRef
	ImpliedFields map[string]string
}

// PackObject builds the cluster-scoped Pack record `up` writes (and `down`,
// via the inventory, deletes). ${GATEWAY_HOST} in urls is replaced with the
// cube's spec.gateway.host[:port] — the one substitution the contract
// allows. The port is included whenever the gateway doesn't listen on 443
// (HTTPS's default), so `kubectl get packs` links are actually clickable
// instead of dialing the bare host on 443 and failing (D11/Task 15.1).
func PackObject(p *Pack, gw config.GatewaySpec, ready bool) *unstructured.Unstructured {
	spec := map[string]any{"version": p.Version, "ready": ready}
	if e := p.Expose; e != nil {
		if len(e.URLs) > 0 {
			host := gw.Host
			if gw.Port != 443 {
				host = fmt.Sprintf("%s:%d", gw.Host, gw.Port)
			}
			urls := make([]any, 0, len(e.URLs))
			for _, u := range e.URLs {
				urls = append(urls, strings.ReplaceAll(u, "${GATEWAY_HOST}", host))
			}
			spec["urls"] = urls
			spec["url"] = urls[0]
		}
		if r := e.AuthSecretRef; r != nil {
			spec["authSecretRef"] = map[string]any{"namespace": r.Namespace, "name": r.Name}
			spec["authSecret"] = r.Namespace + "/" + r.Name
		}
		if len(e.ImpliedFields) > 0 {
			f := map[string]any{}
			for k, v := range e.ImpliedFields {
				f[k] = v
			}
			spec["impliedFields"] = f
		}
	}
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "cube-idp.dev/v1alpha1",
		"kind":       "Pack",
		"metadata":   map[string]any{"name": p.Name},
		"spec":       spec,
	}}
}
