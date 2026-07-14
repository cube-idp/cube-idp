// Package kube holds small, generic Kubernetes API helpers shared across
// cube-idp's commands and orchestrators — currently just a pod port-forward
// tunnel (Task 10), generalized from Phase 1's zot-only
// internal/registry.PortForward so Task 12's git-serving pod can reuse it.
package kube

import (
	"context"
	"fmt"
	"net/http"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
)

// readyDeadline is the hard local deadline for the tunnel to become ready,
// so a caller passing context.Background() can never hang indefinitely.
const readyDeadline = 30 * time.Second

// PortForward tunnels a free local port to the first running pod matching
// selector in ns, targeting podPort, and returns "127.0.0.1:<port>".
// stop() must be deferred by the caller.
//
// Errors are plain (not diag-typed): PortForward has no domain-specific
// CUBE-xxxx code of its own — every caller wraps failures with the code and
// remediation appropriate to what it was tunneling to (e.g.
// internal/registry wraps as CUBE-5002 for the zot tunnel; internal/syncer
// does the same for `cube-idp sync`'s tunnel).
func PortForward(ctx context.Context, cfg *rest.Config, ns, selector string, podPort int) (string, func(), error) {
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return "", nil, fmt.Errorf("cannot build Kubernetes client for port-forward: %w", err)
	}
	pods, err := cs.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{
		LabelSelector: selector, FieldSelector: "status.phase=" + string(corev1.PodRunning)})
	if err != nil {
		return "", nil, fmt.Errorf("cannot list pods matching %q in %s: %w", selector, ns, err)
	}
	if len(pods.Items) == 0 {
		return "", nil, fmt.Errorf("no running pod matching %q in %s to port-forward to", selector, ns)
	}
	req := cs.CoreV1().RESTClient().Post().Resource("pods").
		Namespace(ns).Name(pods.Items[0].Name).SubResource("portforward")
	transport, upgrader, err := spdy.RoundTripperFor(cfg)
	if err != nil {
		return "", nil, fmt.Errorf("cannot build SPDY transport for port-forward: %w", err)
	}
	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, http.MethodPost, req.URL())
	stopCh, readyCh := make(chan struct{}), make(chan struct{})
	fw, err := portforward.New(dialer, []string{fmt.Sprintf("0:%d", podPort)}, stopCh, readyCh, nil, nil)
	if err != nil {
		return "", nil, fmt.Errorf("cannot create port-forwarder: %w", err)
	}
	errCh := make(chan error, 1)
	go func() { errCh <- fw.ForwardPorts() }()
	select {
	case <-readyCh:
	case err := <-errCh:
		close(stopCh)
		return "", nil, fmt.Errorf("port-forward failed before becoming ready: %w", err)
	case <-ctx.Done():
		close(stopCh)
		return "", nil, fmt.Errorf("port-forward canceled before becoming ready: %w", ctx.Err())
	case <-time.After(readyDeadline):
		close(stopCh)
		return "", nil, fmt.Errorf("port-forward did not become ready within %s", readyDeadline)
	}
	ports, err := fw.GetPorts()
	if err != nil || len(ports) == 0 {
		close(stopCh)
		return "", nil, fmt.Errorf("port-forward failed to report its local port: %w", err)
	}
	return fmt.Sprintf("127.0.0.1:%d", ports[0].Local), func() { close(stopCh) }, nil
}
