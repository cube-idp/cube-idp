// Package spoke bootstraps and registers spoke clusters (Phase 5 spec §5).
// cube-idp is a pusher here too: apply RBAC, mint a token, hand the
// credential to the hub engine, exit. No controller, no CRD, no daemon.
package spoke

import (
	"context"
	"fmt"
	"os"
	"time"

	authv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes"

	"github.com/cube-idp/cube-idp/internal/apply"
	"github.com/cube-idp/cube-idp/internal/diag"
	"github.com/cube-idp/cube-idp/internal/kube"
)

const (
	Namespace = "cube-idp-system"
	// tokenTTL is 10 years (GT5). Servers clamp silently; every `up`
	// re-issues, so a clamped token never strands a spoke.
	tokenTTL int64 = 315360000
)

// Credential is everything the hub needs to reach a spoke as
// cube-idp-<engine>: the SA bearer token and the cluster CA. The server
// URL is chosen by the CALLER (S3) — internal kubeconfig URL for kind
// spokes, the context's own URL for existing spokes.
type Credential struct {
	Token  string
	CAData []byte
}

func saName(engineType string) string { return "cube-idp-" + engineType }

// objects returns the three bootstrap objects. Data-only unstructured so
// the existing SSA Applier handles them like everything else cube-idp
// pushes.
func objects(engineType string) []*unstructured.Unstructured {
	ns := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1", "kind": "Namespace",
		"metadata": map[string]any{"name": Namespace},
	}}
	sa := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1", "kind": "ServiceAccount",
		"metadata": map[string]any{"name": saName(engineType), "namespace": Namespace},
	}}
	crb := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "rbac.authorization.k8s.io/v1", "kind": "ClusterRoleBinding",
		"metadata": map[string]any{"name": saName(engineType) + "-admin"},
		"roleRef": map[string]any{
			"apiGroup": "rbac.authorization.k8s.io", "kind": "ClusterRole", "name": "cluster-admin",
		},
		"subjects": []any{map[string]any{
			"kind": "ServiceAccount", "name": saName(engineType), "namespace": Namespace,
		}},
	}}
	return []*unstructured.Unstructured{ns, sa, crb}
}

// Bootstrap idempotently applies namespace cube-idp-system, ServiceAccount
// cube-idp-<engineType>, and ClusterRoleBinding cube-idp-<engineType>-admin
// (→ cluster-admin) on the spoke behind conn, then mints a 10-year
// TokenRequest token (GT5; server may clamp — re-issued on every up).
func Bootstrap(ctx context.Context, conn *kube.Conn, engineType string, timeout time.Duration) (*Credential, error) {
	a, err := apply.New(conn.REST, "spoke-bootstrap")
	if err != nil {
		return nil, err
	}
	if err := a.Apply(ctx, objects(engineType), true, timeout); err != nil {
		return nil, diag.Wrap(err, diag.CodeSpokeBootstrapFailed,
			"spoke RBAC bootstrap failed",
			"check the spoke is reachable and your credentials can create namespaces and clusterrolebindings")
	}
	cs, err := kubernetes.NewForConfig(conn.REST)
	if err != nil {
		return nil, diag.Wrap(err, diag.CodeSpokeBootstrapFailed, "cannot build client for spoke", "verify the spoke kubeconfig")
	}
	ttl := tokenTTL
	tr, err := cs.CoreV1().ServiceAccounts(Namespace).CreateToken(ctx, saName(engineType),
		&authv1.TokenRequest{Spec: authv1.TokenRequestSpec{ExpirationSeconds: &ttl}}, metav1.CreateOptions{})
	if err != nil {
		return nil, diag.Wrap(err, diag.CodeSpokeTokenFailed,
			fmt.Sprintf("token issuance for %s failed", saName(engineType)),
			"the spoke API server must support the TokenRequest API (any supported Kubernetes version does)")
	}
	// CAData is empty when the kubeconfig points at a CA file instead of
	// embedding the bytes (VERIFY-API (b)): fall back to reading CAFile.
	ca := conn.REST.TLSClientConfig.CAData
	if len(ca) == 0 && conn.REST.TLSClientConfig.CAFile != "" {
		ca, err = os.ReadFile(conn.REST.TLSClientConfig.CAFile)
		if err != nil {
			return nil, diag.Wrap(err, diag.CodeSpokeBootstrapFailed, "cannot read spoke CA file", "check the kubeconfig's certificate-authority path")
		}
	}
	return &Credential{Token: tr.Status.Token, CAData: ca}, nil
}
