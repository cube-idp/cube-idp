package trust

import (
	"context"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const baseCorefile = `.:53 {
    errors
    health
    ready
    kubernetes cluster.local in-addr.arpa ip6.arpa {
        pods insecure
    }
    forward . /etc/resolv.conf
    cache 30
}
`

func fakeClientWithCoreDNS(t *testing.T) client.Client {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "coredns", Namespace: "kube-system"},
		Data:       map[string]string{"Corefile": baseCorefile},
	}
	dep := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "coredns", Namespace: "kube-system"}}
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(cm, dep).Build()
}

func getCorefile(t *testing.T, c client.Client) string {
	t.Helper()
	var cm corev1.ConfigMap
	if err := c.Get(context.Background(), client.ObjectKey{Namespace: "kube-system", Name: "coredns"}, &cm); err != nil {
		t.Fatal(err)
	}
	return cm.Data["Corefile"]
}

func TestEnsureCoreDNSRewriteInsertsOnceAndReverts(t *testing.T) {
	c := fakeClientWithCoreDNS(t)
	ctx := context.Background()
	for i := 0; i < 2; i++ { // idempotent
		if err := EnsureCoreDNSRewrite(ctx, c, "cube-idp.localtest.me", "traefik.traefik.svc.cluster.local", time.Second); err != nil {
			t.Fatalf("ensure #%d: %v", i+1, err)
		}
	}
	cf := getCorefile(t, c)
	if strings.Count(cf, "cube-idp:rewrite:begin") != 1 {
		t.Fatalf("rewrite block must appear exactly once:\n%s", cf)
	}
	if !strings.Contains(cf, `(.*)\.cube-idp\.localtest\.me`) ||
		!strings.Contains(cf, "traefik.traefik.svc.cluster.local") {
		t.Fatalf("rewrite content wrong:\n%s", cf)
	}
	if !strings.Contains(cf, "kubernetes cluster.local") {
		t.Fatal("original Corefile content lost")
	}
	if err := RemoveCoreDNSRewrite(ctx, c, time.Second); err != nil {
		t.Fatal(err)
	}
	if got := getCorefile(t, c); got != baseCorefile {
		t.Fatalf("remove must restore the original Corefile:\n%s", got)
	}
	// removing again is a no-op
	if err := RemoveCoreDNSRewrite(ctx, c, time.Second); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureCoreDNSRewriteHostChange(t *testing.T) {
	c := fakeClientWithCoreDNS(t)
	ctx := context.Background()
	if err := EnsureCoreDNSRewrite(ctx, c, "old.example.com", "traefik.traefik.svc.cluster.local", time.Second); err != nil {
		t.Fatal(err)
	}
	if err := EnsureCoreDNSRewrite(ctx, c, "new.example.com", "traefik.traefik.svc.cluster.local", time.Second); err != nil {
		t.Fatal(err)
	}
	cf := getCorefile(t, c)
	if strings.Contains(cf, "old.example.com") || strings.Count(cf, "cube-idp:rewrite:begin") != 1 {
		t.Fatalf("host change must replace the block:\n%s", cf)
	}
}
