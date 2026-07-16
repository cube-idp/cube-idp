// Package contracttest is the shared ClusterProvider contract (spec §5).
// Every cluster-creating provider (kindp, k3dp, future Talos/vcluster) calls
// Run from its own test package. The contract is behavioral: idempotent
// Ensure, truthful Exists, non-empty Kubeconfig for a live cluster, clean
// Delete, and a Diagnose that never panics.
package contracttest

import (
	"context"
	"os"
	"testing"
	"time"

	"k8s.io/client-go/discovery"

	"github.com/cube-idp/cube-idp/internal/cluster"
	"github.com/cube-idp/cube-idp/internal/config"
	"github.com/cube-idp/cube-idp/internal/diag"
)

const gate = "CUBE_IDP_PROVIDER_E2E"

func Run(t *testing.T, p cluster.Provider, spec config.ClusterSpec) {
	t.Helper()
	if os.Getenv(gate) != "1" {
		t.Skipf("set %s=1 to run the live provider contract (needs a container runtime)", gate)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute) // deadline rule
	defer cancel()
	name := "contract-" + spec.Provider

	// Pre-state: the cluster must not exist.
	if ok, err := p.Exists(ctx, name); err != nil || ok {
		t.Fatalf("pre-state Exists = %v, %v (leftover cluster? delete it first)", ok, err)
	}

	// Ensure creates…
	conn, err := p.Ensure(ctx, name, spec)
	if err != nil {
		t.Fatalf("Ensure (create): %v", err)
	}
	t.Cleanup(func() { _ = p.Delete(context.Background(), name) }) // never leak clusters
	if conn == nil || len(conn.Kubeconfig) == 0 || conn.REST == nil {
		t.Fatalf("Ensure returned an unusable Conn: %+v", conn)
	}

	// The REST config must actually reach the API server — not merely be
	// non-nil. A provider that hands back a structurally-valid but
	// unreachable config (e.g. the k3d kubeconfig-port bug, which produced
	// https://0.0.0.0 with no port) passed the nil-check above yet failed
	// the very first live API call in `up`. Dial the API here so a dead REST
	// config fails the contract instead of leaking to production.
	{
		rc := *conn.REST
		rc.Timeout = 90 * time.Second
		dc, err := discovery.NewDiscoveryClientForConfig(&rc)
		if err != nil {
			t.Fatalf("build discovery client from conn.REST: %v", err)
		}
		if _, err := dc.ServerVersion(); err != nil {
			t.Fatalf("conn.REST cannot reach the API server (host %q): %v", conn.REST.Host, err)
		}
	}

	// …and is idempotent.
	if _, err := p.Ensure(ctx, name, spec); err != nil {
		t.Fatalf("Ensure (idempotent re-run): %v", err)
	}
	if ok, err := p.Exists(ctx, name); err != nil || !ok {
		t.Fatalf("Exists after Ensure = %v, %v", ok, err)
	}

	// Kubeconfig for a live cluster is retrievable independently of Ensure.
	kc, err := p.Kubeconfig(ctx, name)
	if err != nil || len(kc) == 0 {
		t.Fatalf("Kubeconfig: %v (len %d)", err, len(kc))
	}

	// Diagnose never panics and returns no error-severity findings on a
	// healthy cluster. (diag.SeverityError verified 2026-07-14 — import
	// github.com/cube-idp/cube-idp/internal/diag.)
	for _, f := range p.Diagnose(ctx, name) {
		if f.Severity == diag.SeverityError {
			t.Fatalf("Diagnose reported an error on a healthy cluster: %+v", f)
		}
	}

	// Delete tears down; Exists goes false.
	if err := p.Delete(ctx, name); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if ok, _ := p.Exists(ctx, name); ok {
		t.Fatal("Exists still true after Delete")
	}
}
