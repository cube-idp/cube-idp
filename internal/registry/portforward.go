package registry

import (
	"context"

	"k8s.io/client-go/rest"

	"github.com/cube-idp/cube-idp/internal/apply"
	"github.com/cube-idp/cube-idp/internal/diag"
	"github.com/cube-idp/cube-idp/internal/kube"
)

// PortForward tunnels a free local port to the zot pod and returns
// "127.0.0.1:<port>". stop() must be deferred by the caller.
//
// Task 10: this is now a thin delegate to the generalized
// internal/kube.PortForward (ns=cube-idp-system, selector=app=zot,
// podPort=5000 — the exact parameters Phase 1's implementation hard-coded),
// wrapping its plain error as CUBE-5002 so callers keep the same typed
// failure they always got.
func PortForward(ctx context.Context, cfg *rest.Config) (string, func(), error) {
	addr, stop, err := kube.PortForward(ctx, cfg, apply.SystemNamespace, "app=zot", 5000)
	if err != nil {
		return "", nil, diag.Wrap(err, diag.CodePortForwardFail, "port-forward to zot failed",
			"re-run `cube-idp up`; check `kubectl -n cube-idp-system get pods`")
	}
	return addr, stop, nil
}
