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

// ExposeURLs returns p's expose.urls with ${GATEWAY_HOST} substituted for
// the cube's spec.gateway.host[:port] — the one substitution the D11
// contract allows. The port is included whenever the gateway doesn't listen
// on 443 (HTTPS's default), so the resulting links are actually clickable
// instead of dialing the bare host on 443 and failing (Task 15.1). Returns
// nil if p declares no expose block or no urls. Shared by PackObject (the
// Pack record's spec.urls/spec.url) and up.Run's Task 15.3c access summary —
// one substitution, used everywhere a pack's URLs are shown.
func ExposeURLs(p *Pack, gw config.GatewaySpec) []string {
	if p.Expose == nil || len(p.Expose.URLs) == 0 {
		return nil
	}
	host := gw.Host
	if gw.Port != 443 {
		host = fmt.Sprintf("%s:%d", gw.Host, gw.Port)
	}
	urls := make([]string, 0, len(p.Expose.URLs))
	for _, u := range p.Expose.URLs {
		urls = append(urls, strings.ReplaceAll(u, "${GATEWAY_HOST}", host))
	}
	return urls
}

// PackObject builds the cluster-scoped Pack record `up` writes (and `down`,
// via the inventory, deletes).
func PackObject(p *Pack, gw config.GatewaySpec, ready bool) *unstructured.Unstructured {
	spec := map[string]any{"version": p.Version, "ready": ready}
	if urls := ExposeURLs(p, gw); len(urls) > 0 {
		anyURLs := make([]any, len(urls))
		for i, u := range urls {
			anyURLs[i] = u
		}
		spec["urls"] = anyURLs
		spec["url"] = urls[0]
	}
	if e := p.Expose; e != nil {
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
