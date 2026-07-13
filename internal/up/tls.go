package up

import (
	"context"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/rafpe/cube-idp/internal/apply"
	"github.com/rafpe/cube-idp/internal/config"
	"github.com/rafpe/cube-idp/internal/trust"
)

// gatewayTLSValidity is how long an issued gateway leaf cert is valid for.
// ensureGatewayTLS re-issues well before expiry (see its 30-day margin), so
// this only bounds how stale a cert can get if `up` is never re-run.
const gatewayTLSValidity = 365 * 24 * time.Hour

// gatewayTLSObjects builds the Namespace + kubernetes.io/tls Secret the
// gateway's websecure listener terminates with. The namespace equals the
// gateway pack name by pack convention (traefik pack -> ns "traefik"; see
// packs/traefik/chart.yaml and packs/traefik/manifests/10-gateway.yaml,
// both of which fix namespace: traefik).
func gatewayTLSObjects(ca *trust.CA, gw config.GatewaySpec, validity time.Duration) ([]*unstructured.Unstructured, error) {
	hosts := []string{gw.Host, "*." + gw.Host}
	certPEM, keyPEM, err := ca.IssueServerCert(hosts, validity)
	if err != nil {
		return nil, err
	}
	ns := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1", "kind": "Namespace",
		"metadata": map[string]any{"name": gw.Pack},
	}}
	sec := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1", "kind": "Secret",
		"metadata": map[string]any{"name": "cube-idp-gateway-tls", "namespace": gw.Pack},
		"type":     "kubernetes.io/tls",
		"stringData": map[string]any{
			"tls.crt": string(certPEM),
			"tls.key": string(keyPEM),
		},
	}}
	return []*unstructured.Unstructured{ns, sec}, nil
}

// ensureGatewayTLS is idempotent: it reuses a live secret whose cert still
// covers the hosts with >30 days left, so repeated `up` runs (and `diff`)
// see no churn.
func ensureGatewayTLS(ctx context.Context, a *apply.Applier, gw config.GatewaySpec) error {
	dir, err := trust.Dir()
	if err != nil {
		return err
	}
	ca, err := trust.EnsureCA(dir)
	if err != nil {
		return err
	}
	var existing corev1.Secret
	err = a.Client().Get(ctx, client.ObjectKey{Namespace: gw.Pack, Name: "cube-idp-gateway-tls"}, &existing)
	if err == nil && ca.LeafStillValid(existing.Data["tls.crt"], []string{gw.Host, "*." + gw.Host}, 30*24*time.Hour) {
		return nil
	}
	objs, err := gatewayTLSObjects(ca, gw, gatewayTLSValidity)
	if err != nil {
		return err
	}
	if err := a.Apply(ctx, objs, true, time.Minute); err != nil {
		return err
	}
	return a.RecordInventory(ctx, objs)
}
