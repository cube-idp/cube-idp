package pack

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/cube-idp/cube-idp/internal/config"
)

// SecretRef locates a Kubernetes Secret a pack's expose block points at.
type SecretRef struct{ Namespace, Name string }

// GatewayService is the optional pack.cue declaration of a gateway pack's
// DATA-PLANE Service (spec §5.7b, R7b): the CoreDNS *.<host> rewrite target
// `up` points at once the pack is resolved. Absent, `up` falls back to
// today's <pack>.<pack>.svc default (traefik: zero migration). Parsed from
// pack.cue's optional gatewayService: block, the same optional-and-typed
// shape as Expose.
type GatewayService struct{ Name, Namespace string }

// Expose is the D11 contract: how a pack declares its endpoints and
// credentials — in data, never in Go. Parsed from pack.cue's optional
// expose: block; rendered by `up` into the pack's Pack record.
type Expose struct {
	URLs          []string
	AuthSecretRef *SecretRef
	ImpliedFields map[string]string
}

// GatewayHostString renders gw as the ${GATEWAY_HOST} substitution value:
// host[:port], with the port omitted whenever the gateway listens on 443
// (HTTPS's default) so the resulting links are actually clickable instead
// of dialing the bare host on 443 and failing (Task 15.1). Shared by
// ExposeURLs and the D15 chart-value/manifest substitution below — one
// definition of "the gateway's host string", used everywhere it's needed.
// Exported (Task 12) so cmd/repo.go can print the same https gateway form
// for the printed operator clone URL without duplicating the port-omission
// rule.
func GatewayHostString(gw config.GatewaySpec) string {
	if gw.Port != 443 {
		return fmt.Sprintf("%s:%d", gw.Host, gw.Port)
	}
	return gw.Host
}

// substitute performs the D15 gateway substitution on s: ${GATEWAY_HOST}
// expands to gw's host[:port] (GatewayHostString), ${GATEWAY_FQDN} expands
// to the bare gw.Host (for Gateway API `hostnames:` fields, which cannot
// carry ports), and ${GATEWAY_PACK} expands to gw.Pack — the gateway pack
// name, which is also its namespace by convention (gatewayServiceFQDN,
// internal/up). ${GATEWAY_PACK} exists for F9: pack HTTPRoutes must parent
// to the Gateway in the gateway pack's namespace (e.g. envoy-gateway), not
// a hardcoded "traefik", or Attached Routes stays 0 and TLS/HTTP resets.
// A zero gw (Host == "") performs no substitution — the literal tokens pass
// through untouched, which is what Render's gateway-less default relies on.
func substitute(s string, gw config.GatewaySpec) string {
	if gw.Host == "" {
		return s
	}
	s = strings.ReplaceAll(s, "${GATEWAY_HOST}", GatewayHostString(gw))
	s = strings.ReplaceAll(s, "${GATEWAY_FQDN}", gw.Host)
	s = strings.ReplaceAll(s, "${GATEWAY_PACK}", gw.Pack)
	return s
}

// ExposeURLs returns p's expose.urls with ${GATEWAY_HOST} substituted for
// the cube's spec.gateway.host[:port] — the one substitution the D11
// contract originally allowed, now one of two (see substitute) shared with
// D15's chart-value/manifest substitution. Returns nil if p declares no
// expose block or no urls. Shared by PackObject (the Pack record's
// spec.urls/spec.url) and up.Run's Task 15.3c access summary — one
// substitution, used everywhere a pack's URLs are shown.
func ExposeURLs(p *Pack, gw config.GatewaySpec) []string {
	if p.Expose == nil || len(p.Expose.URLs) == 0 {
		return nil
	}
	urls := make([]string, 0, len(p.Expose.URLs))
	for _, u := range p.Expose.URLs {
		urls = append(urls, substitute(u, gw))
	}
	return urls
}

// PackObject builds the cluster-scoped Pack record `up` writes (and `down`,
// via the inventory, deletes). customized is GT15's operator-visibility
// bit (U4): true when the pack's PackRef carried non-empty values or
// extraManifests — the caller decides, since only it holds the ref. The
// record always carries spec.customized as "yes"/"no" (never absent) so
// the CUSTOMIZED printer column renders for stock packs too. delivery is
// GT19's twin bit (P7), the ref's delivery mode verbatim: an empty
// PackRef.Delivery maps to "oci" HERE, in the record writer, so every
// pack shows a value and repo-delivered packs stand out in the DELIVERY
// printer column.
func PackObject(p *Pack, gw config.GatewaySpec, ready, customized bool, delivery string) *unstructured.Unstructured {
	customizedStr := "no"
	if customized {
		customizedStr = "yes"
	}
	if delivery == "" {
		delivery = "oci"
	}
	spec := map[string]any{"version": p.Version, "ready": ready, "customized": customizedStr, "delivery": delivery}
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
