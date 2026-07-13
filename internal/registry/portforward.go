package registry

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

	"github.com/rafpe/cube-idp/internal/apply"
	"github.com/rafpe/cube-idp/internal/diag"
)

// readyDeadline is the hard local deadline for the tunnel to become ready,
// so a caller passing context.Background() can never hang indefinitely.
const readyDeadline = 30 * time.Second

// PortForward tunnels a free local port to the zot pod and returns
// "127.0.0.1:<port>". stop() must be deferred by the caller.
func PortForward(ctx context.Context, cfg *rest.Config) (string, func(), error) {
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return "", nil, diag.Wrap(err, "CUBE-5002", "cannot build Kubernetes client for port-forward",
			"check kubeconfig and cluster connectivity")
	}
	pods, err := cs.CoreV1().Pods(apply.SystemNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: "app=zot", FieldSelector: "status.phase=" + string(corev1.PodRunning)})
	if err != nil {
		return "", nil, diag.Wrap(err, "CUBE-5002", "cannot list zot pods", "check cluster connectivity")
	}
	if len(pods.Items) == 0 {
		return "", nil, diag.New("CUBE-5002", "no running zot pod to port-forward to",
			"re-run `cube-idp up`; check `kubectl -n cube-idp-system get pods`")
	}
	req := cs.CoreV1().RESTClient().Post().Resource("pods").
		Namespace(apply.SystemNamespace).Name(pods.Items[0].Name).SubResource("portforward")
	transport, upgrader, err := spdy.RoundTripperFor(cfg)
	if err != nil {
		return "", nil, diag.Wrap(err, "CUBE-5002", "cannot build SPDY transport for port-forward",
			"check kubeconfig and cluster connectivity")
	}
	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, http.MethodPost, req.URL())
	stopCh, readyCh := make(chan struct{}), make(chan struct{})
	fw, err := portforward.New(dialer, []string{"0:5000"}, stopCh, readyCh, nil, nil)
	if err != nil {
		return "", nil, diag.Wrap(err, "CUBE-5002", "cannot create port-forwarder to zot",
			"retry `cube-idp up`")
	}
	errCh := make(chan error, 1)
	go func() { errCh <- fw.ForwardPorts() }()
	select {
	case <-readyCh:
	case err := <-errCh:
		close(stopCh)
		return "", nil, diag.Wrap(err, "CUBE-5002", "port-forward to zot failed before becoming ready",
			"re-run `cube-idp up`; check `kubectl -n cube-idp-system get pods`")
	case <-ctx.Done():
		close(stopCh)
		return "", nil, diag.Wrap(ctx.Err(), "CUBE-5002", "port-forward to zot canceled before becoming ready",
			"re-run `cube-idp up`; check `kubectl -n cube-idp-system get pods`")
	case <-time.After(readyDeadline):
		close(stopCh)
		return "", nil, diag.New("CUBE-5002", "port-forward to zot did not become ready within 30s",
			"check `kubectl -n cube-idp-system get pods` and node health")
	}
	ports, err := fw.GetPorts()
	if err != nil || len(ports) == 0 {
		close(stopCh)
		return "", nil, diag.Wrap(err, "CUBE-5002", "port-forward to zot failed", "retry `cube-idp up`")
	}
	return fmt.Sprintf("127.0.0.1:%d", ports[0].Local), func() { close(stopCh) }, nil
}
