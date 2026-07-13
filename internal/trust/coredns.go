package trust

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/rafpe/cube-idp/internal/diag"
)

const (
	rewriteBegin = "    # cube-idp:rewrite:begin"
	rewriteEnd   = "    # cube-idp:rewrite:end"
)

// EnsureCoreDNSRewrite makes *.<host> resolve to the gateway Service inside
// the cluster (D6 canonical hostname). Idempotent; replaces its own block on
// host change; restarts CoreDNS and waits (deadline-bound) for the rollout.
func EnsureCoreDNSRewrite(ctx context.Context, c client.Client, host, targetFQDN string, timeout time.Duration) error {
	block := fmt.Sprintf("%s\n    rewrite stop {\n        name regex (.*)\\.%s %s\n        answer auto\n    }\n%s",
		rewriteBegin, regexp.QuoteMeta(host), targetFQDN, rewriteEnd)
	return patchCorefile(ctx, c, timeout, func(corefile string) (string, error) {
		cleaned := removeManagedBlock(corefile)
		idx := strings.Index(cleaned, "\n    ready")
		if idx < 0 {
			idx = strings.Index(cleaned, "{") // fall back: right after the server block opens
			if idx < 0 {
				return "", diag.New(diag.CodeTrustCoreDNSFail, "CoreDNS Corefile has an unexpected shape",
					"inspect `kubectl -n kube-system get cm coredns -o yaml`; set spec.gateway.host to a name your DNS already resolves to skip the rewrite")
			}
		} else {
			idx += len("\n    ready")
		}
		return cleaned[:idx+1] + block + "\n" + cleaned[idx+1:], nil
	})
}

// RemoveCoreDNSRewrite reverts EnsureCoreDNSRewrite's patch, restoring the
// original Corefile. Idempotent: a no-op when no managed block is present.
func RemoveCoreDNSRewrite(ctx context.Context, c client.Client, timeout time.Duration) error {
	return patchCorefile(ctx, c, timeout, func(corefile string) (string, error) {
		return removeManagedBlock(corefile), nil
	})
}

func removeManagedBlock(corefile string) string {
	b := strings.Index(corefile, rewriteBegin)
	e := strings.Index(corefile, rewriteEnd)
	if b < 0 || e < 0 {
		return corefile
	}
	// corefile[:b] keeps the newline that precedes the managed block (it
	// belongs to whatever line came before, e.g. "ready\n"); skipping
	// rewriteEnd's own trailing newline via +1 restores the original layout.
	return corefile[:b] + corefile[e+len(rewriteEnd)+1:]
}

func patchCorefile(ctx context.Context, c client.Client, timeout time.Duration, edit func(string) (string, error)) error {
	var cm corev1.ConfigMap
	if err := c.Get(ctx, client.ObjectKey{Namespace: "kube-system", Name: "coredns"}, &cm); err != nil {
		return diag.Wrap(err, diag.CodeTrustCoreDNSFail, "cannot read the CoreDNS ConfigMap",
			"non-CoreDNS clusters are not supported for the canonical hostname; set spec.gateway.host to a resolvable name")
	}
	updated, err := edit(cm.Data["Corefile"])
	if err != nil {
		return err
	}
	if updated == cm.Data["Corefile"] {
		return nil // no change, no restart
	}
	cm.Data["Corefile"] = updated
	if err := c.Update(ctx, &cm); err != nil {
		return diag.Wrap(err, diag.CodeTrustCoreDNSFail, "cannot update the CoreDNS ConfigMap", "check RBAC on kube-system")
	}
	// restart CoreDNS so the change takes effect, then wait (hard deadline)
	var dep appsv1.Deployment
	if err := c.Get(ctx, client.ObjectKey{Namespace: "kube-system", Name: "coredns"}, &dep); err != nil {
		return diag.Wrap(err, diag.CodeTrustCoreDNSFail, "cannot find the CoreDNS Deployment", "check the cluster's DNS setup")
	}
	if dep.Spec.Template.Annotations == nil {
		dep.Spec.Template.Annotations = map[string]string{}
	}
	dep.Spec.Template.Annotations["cube-idp.dev/restartedAt"] = time.Now().Format(time.RFC3339)
	if err := c.Update(ctx, &dep); err != nil {
		return diag.Wrap(err, diag.CodeTrustCoreDNSFail, "cannot restart CoreDNS", "check RBAC on kube-system")
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		var d appsv1.Deployment
		if err := c.Get(ctx, client.ObjectKey{Namespace: "kube-system", Name: "coredns"}, &d); err == nil {
			if d.Status.ObservedGeneration >= d.Generation && d.Status.UpdatedReplicas == d.Status.Replicas && d.Status.UnavailableReplicas == 0 {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return diag.Wrap(ctx.Err(), diag.CodeTrustCoreDNSFail, "interrupted while waiting for CoreDNS to roll out", "re-run the command")
		case <-time.After(2 * time.Second):
		}
	}
	return diag.New(diag.CodeTrustCoreDNSFail, "CoreDNS did not become ready after the rewrite within the deadline",
		"inspect `kubectl -n kube-system rollout status deploy/coredns`; the rewrite is applied and will work once CoreDNS recovers")
}
