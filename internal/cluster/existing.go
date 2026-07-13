package cluster

import (
	"context"
	"fmt"

	"k8s.io/client-go/discovery"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/rafpe/cube-idp/internal/config"
	"github.com/rafpe/cube-idp/internal/diag"
	"github.com/rafpe/cube-idp/internal/kube"
)

// existing targets a pre-existing cluster through a kubeconfig context.
// It never creates or deletes clusters; Delete is a documented no-op
// (down removes only cube-idp-managed resources, spec §4.3).
type existing struct{}

func (e *existing) load(kctx string) (clientcmd.ClientConfig, error) {
	rules := clientcmd.NewDefaultClientConfigLoadingRules() // honors KUBECONFIG
	overrides := &clientcmd.ConfigOverrides{CurrentContext: kctx}
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, overrides), nil
}

func (e *existing) Ensure(ctx context.Context, name string, spec config.ClusterSpec) (*kube.Conn, error) {
	cc, _ := e.load(spec.Context)
	raw, err := cc.RawConfig()
	if err != nil {
		return nil, diag.Wrap(err, diag.CodeKubeconfigError, "cannot load kubeconfig", "check $KUBECONFIG")
	}
	kctx := spec.Context
	if kctx == "" {
		kctx = raw.CurrentContext
	}
	if _, ok := raw.Contexts[kctx]; !ok {
		return nil, diag.New(diag.CodeKubeconfigError, fmt.Sprintf("kubeconfig context %q not found", kctx),
			"run `kubectl config get-contexts` and set spec.cluster.context to one of them")
	}
	restCfg, err := cc.ClientConfig()
	if err != nil {
		return nil, diag.Wrap(err, diag.CodeKubeconfigError, "cannot build client config", "check $KUBECONFIG")
	}
	dc, err := discovery.NewDiscoveryClientForConfig(restCfg)
	if err == nil {
		_, err = dc.ServerVersion()
	}
	if err != nil {
		return nil, diag.Wrap(err, diag.CodeKubeUnreachable, fmt.Sprintf("cluster behind context %q is unreachable", kctx),
			"check that the cluster is running and the context credentials are valid")
	}
	kubeconfig, err := clientcmd.Write(raw)
	if err != nil {
		return nil, diag.Wrap(err, diag.CodeKubeconfigError, "cannot serialize kubeconfig", "check $KUBECONFIG")
	}
	return &kube.Conn{Kubeconfig: kubeconfig, Context: kctx, REST: restCfg}, nil
}

func (e *existing) Delete(ctx context.Context, name string) error { return nil }

func (e *existing) Exists(ctx context.Context, name string) (bool, error) {
	cc, _ := e.load("")
	raw, err := cc.RawConfig()
	if err != nil {
		return false, nil
	}
	return len(raw.Contexts) > 0, nil
}

func (e *existing) Kubeconfig(ctx context.Context, name string) ([]byte, error) {
	cc, _ := e.load("")
	raw, err := cc.RawConfig()
	if err != nil {
		return nil, diag.Wrap(err, diag.CodeKubeconfigError, "cannot load kubeconfig", "check $KUBECONFIG")
	}
	return clientcmd.Write(raw)
}

func (e *existing) Diagnose(ctx context.Context, name string) []diag.Finding {
	if _, err := e.Ensure(ctx, name, config.ClusterSpec{Provider: "existing"}); err != nil {
		return []diag.Finding{{Code: diag.CodeKubeUnreachable, Severity: diag.SeverityError,
			Message: err.Error(), Remediation: "verify kubeconfig and cluster health"}}
	}
	return nil
}
