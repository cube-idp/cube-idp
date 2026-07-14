// Package up orchestrates `cube-idp up` (spec §4.3). It sequences the
// already-tested units and owns user-facing progress output. It has no
// logic of its own beyond ordering and timeouts — keep it that way.
package up

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/rafpe/cube-idp/internal/apply"
	"github.com/rafpe/cube-idp/internal/cluster"
	"github.com/rafpe/cube-idp/internal/config"
	"github.com/rafpe/cube-idp/internal/diag"
	"github.com/rafpe/cube-idp/internal/engine"
	enginefactory "github.com/rafpe/cube-idp/internal/engine/factory"
	"github.com/rafpe/cube-idp/internal/lock"
	"github.com/rafpe/cube-idp/internal/oci"
	"github.com/rafpe/cube-idp/internal/pack"
	"github.com/rafpe/cube-idp/internal/registry"
	"github.com/rafpe/cube-idp/internal/trust"
	"github.com/rafpe/cube-idp/internal/ui"
)

const dnsTimeout = 2 * time.Minute

const (
	clusterTimeout = 3 * time.Minute
	applyTimeout   = 2 * time.Minute
	healthTimeout  = 5 * time.Minute
	healthPoll     = 5 * time.Second
)

// Run drives the full up sequence for the cube.yaml at cfgPath, writing
// progress to out: load config -> ensure the local CA (D12: before any
// cluster artifact references the trust root) -> ensure cluster -> install
// registry -> install engine -> ensure the gateway TLS secret -> port-forward
// the registry -> fetch/render/push/deliver every pack (gateway first) ->
// wait for engine-reported health -> print a success summary.
func Run(ctx context.Context, cfgPath string, out io.Writer) error {
	cube, err := config.Load(cfgPath)
	if err != nil {
		return err
	}
	step(out, "config", "cube %q loaded and validated", cube.Metadata.Name)

	// D12 ("cert material is generated before cluster creation"): ensure the
	// local CA — adopting an existing mkcert root if present — before
	// ClusterProvider.Ensure runs, so the kind provider can mount it into
	// containerd certs.d at cluster-create time (Task 10) and no cluster
	// artifact ever references the trust root before it exists.
	caDir, err := trust.Dir()
	if err != nil {
		return err
	}
	ca, err := trust.EnsureCA(caDir)
	if err != nil {
		return err
	}
	step(out, "ca", "local CA ready (%s)", ca.CertPath)

	prov, err := cluster.New(cube.Spec.Cluster, cube.Spec.Gateway)
	if err != nil {
		return err
	}
	clusterCtx, cancel := context.WithTimeout(ctx, clusterTimeout)
	conn, err := prov.Ensure(clusterCtx, cube.Metadata.Name, cube.Spec.Cluster)
	cancel()
	if err != nil {
		return err
	}
	step(out, "cluster", "%s cluster ready (context %s)", cube.Spec.Cluster.Provider, conn.Context)

	a, err := apply.New(conn.REST, cube.Metadata.Name)
	if err != nil {
		return err
	}
	eng, err := enginefactory.New(cube.Spec.Engine.Type)
	if err != nil {
		return err
	}

	if err := registry.Install(ctx, a, applyTimeout); err != nil {
		return err
	}
	regObjs, err := registry.Manifests()
	if err != nil {
		return err
	}
	if err := a.RecordInventory(ctx, regObjs); err != nil {
		return err
	}
	step(out, "registry", "zot ready at %s", registry.InClusterURL)

	// D11: the inert Pack CRD must exist before any Pack record is written,
	// so it goes in right after the registry — before the engine and the
	// pack delivery loop below. wait=true (kstatus) blocks until the API
	// server has Established it; no controller ever reconciles it further.
	crd, err := pack.CRD()
	if err != nil {
		return err
	}
	crdObjs := []*unstructured.Unstructured{crd}
	if err := a.Apply(ctx, crdObjs, true, applyTimeout); err != nil {
		return err
	}
	if err := a.RecordInventory(ctx, crdObjs); err != nil {
		return err
	}
	step(out, "packs-crd", "Pack CRD established")

	if err := eng.Install(ctx, a, applyTimeout); err != nil {
		return err
	}
	installObjs, err := eng.InstallManifests()
	if err != nil {
		return err
	}
	if err := a.RecordInventory(ctx, installObjs); err != nil {
		return err
	}
	step(out, "engine", "%s installed", cube.Spec.Engine.Type)

	// The gateway pack's websecure listener references this secret by name
	// (packs/traefik/manifests/10-gateway.yaml); it must exist before the
	// engine reconciles the Gateway, so this runs before the pack loop.
	if err := ensureGatewayTLS(ctx, a, cube.Spec.Gateway); err != nil {
		return err
	}
	step(out, "tls", "gateway certificate ready (CA: run `cube-idp trust` to make browsers trust it)")

	tunnelAddr, stop, err := registry.PortForward(ctx, conn.REST)
	if err != nil {
		return err
	}
	defer stop()

	dir, err := pack.DefaultCacheDir()
	if err != nil {
		return err
	}

	// Gateway pack goes first — everything else depends on ingress existing.
	refs := append([]config.PackRef{{Ref: cube.Spec.Gateway.PackRef()}}, cube.Spec.Packs...)
	var entries []lock.Entry
	var packs []*pack.Pack // kept in lockstep with entries: Task 12.5 needs each Pack's Expose after waitHealthy
	for _, pr := range refs {
		p, err := pack.Fetch(ctx, pr.Ref, dir)
		if err != nil {
			return err
		}
		packs = append(packs, p)
		rendered, err := p.Render(pr.Values)
		if err != nil {
			return err
		}
		artifact, err := oci.PushRendered(ctx, rendered, tunnelAddr)
		if err != nil {
			return err
		}
		rh, err := lock.RenderedHash(rendered.Objects)
		if err != nil {
			return err
		}
		entries = append(entries, lock.Entry{
			Ref:          pr.Ref,
			Name:         rendered.Name,
			Version:      rendered.Version,
			Resolved:     p.Pinned,
			RenderedHash: rh,
			Images:       lock.ImagesFrom(rendered.Objects),
		})
		deliverObjs, err := eng.Deliver(ctx, rendered, artifact)
		if err != nil {
			return err
		}
		if err := a.Apply(ctx, deliverObjs, false, applyTimeout); err != nil {
			return err
		}
		if err := a.RecordInventory(ctx, deliverObjs); err != nil {
			return err
		}
		step(out, "pack", "%s@%s delivered", rendered.Name, rendered.Version)
	}

	lf := &lock.File{APIVersion: "cube-idp.dev/v1alpha1", Kind: "CubeLock",
		Engine: lock.EngineLock{Type: cube.Spec.Engine.Type}, Packs: entries}
	if err := lock.Write(lock.PathFor(cfgPath), lf); err != nil {
		return err
	}
	step(out, "lock", "cube.lock written (%d packs)", len(entries))

	// D6 canonical hostname: route registry.<host> through the gateway (for
	// host-side docker/oras push), then make *.<host> resolve in-cluster to
	// the same gateway Service so pod-side clients use identical URLs.
	route := registry.GatewayRoute(cube.Spec.Gateway.Host)
	if err := a.Apply(ctx, []*unstructured.Unstructured{route}, false, applyTimeout); err != nil {
		return err
	}
	if err := a.RecordInventory(ctx, []*unstructured.Unstructured{route}); err != nil {
		return err
	}
	if err := trust.EnsureCoreDNSRewrite(ctx, a.Client(), cube.Spec.Gateway.Host,
		gatewayServiceFQDN(cube.Spec.Gateway), dnsTimeout); err != nil {
		return err
	}
	step(out, "dns", "*.%s resolves to the gateway in-cluster", cube.Spec.Gateway.Host)

	if err := waitHealthy(ctx, eng, a, out, healthTimeout); err != nil {
		return err
	}

	// D11: write each pack's discoverability record now that health is
	// known. waitHealthy polls eng.Health internally but doesn't return the
	// final slice, so ask once more here — cheap, and every reported
	// component was already Ready one poll ago.
	health, err := eng.Health(ctx, a)
	if err != nil {
		return err
	}
	healthByName := make(map[string]bool, len(health))
	for _, h := range health {
		healthByName[h.Name] = h.Ready
	}
	// "cube-idp-"+name is the Deliver object name convention both engines
	// use (internal/engine/flux/deliver.go, internal/engine/argocd/deliver.go).
	packObjs := make([]*unstructured.Unstructured, 0, len(packs))
	for _, p := range packs {
		packObjs = append(packObjs, pack.PackObject(p, cube.Spec.Gateway.Host, healthByName["cube-idp-"+p.Name]))
	}
	if err := a.Apply(ctx, packObjs, false, applyTimeout); err != nil {
		return err
	}
	if err := a.RecordInventory(ctx, packObjs); err != nil {
		return err
	}
	step(out, "packs", "%d pack records written — try `kubectl get packs`", len(packObjs))

	// Phase 2: the gateway's websecure listener terminates TLS with a
	// CA-issued cert (D6/D12), so this URL is genuinely HTTPS. Browsers only
	// show a green lock once the CA is trusted — `cube-idp trust` does that.
	fmt.Fprintf(out, "\n✔ cube %q is up — https://%s:%d\n  credentials: cube-idp get secrets\n",
		cube.Metadata.Name, cube.Spec.Gateway.Host, cube.Spec.Gateway.Port)
	return nil
}

// gatewayServiceFQDN returns the in-cluster DNS name of the gateway pack's
// Service, the CoreDNS rewrite target for *.<gw.Host> (D6). Hardcoded to the
// traefik chart's fullname convention (packs/traefik/chart.yaml: releaseName
// "traefik" == chart name "traefik" -> fullname "traefik"), so the Service
// lands at traefik.traefik.svc.cluster.local — verified against the phase-1
// chart values (checkpoint 0.14). gw.Pack doubles as both name and
// namespace, matching that chart's install (namespace: traefik).
func gatewayServiceFQDN(gw config.GatewaySpec) string {
	return fmt.Sprintf("%s.%s.svc.cluster.local", gw.Pack, gw.Pack)
}

// waitHealthy polls eng.Health every healthPoll until every reported
// component is Ready or timeout elapses. Zero components reported (e.g.
// right after delivering packs, before the engine has reconciled anything)
// is treated as not-ready rather than vacuously healthy — the "no infinite
// spinner" rule still applies: on timeout, CUBE-3004 lists every unready
// component's name and message so the user knows what to look at.
func waitHealthy(ctx context.Context, eng engine.Engine, a *apply.Applier, out io.Writer, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		health, err := eng.Health(ctx, a)
		if err != nil {
			return err
		}
		if allReady(health) {
			step(out, "health", "%d component(s) ready", len(health))
			return nil
		}
		if time.Now().After(deadline) {
			return diag.New(diag.CodeEngineHealthTimeout,
				fmt.Sprintf("timed out after %s waiting for components to become healthy: %s",
					timeout, unreadySummary(health)),
				"re-run `cube-idp up` (idempotent); inspect the listed components with kubectl")
		}
		select {
		case <-ctx.Done():
			return diag.Wrap(ctx.Err(), diag.CodeEngineHealthTimeout, "health wait aborted before completion",
				"re-run `cube-idp up` — it is idempotent and resumes where it left off")
		case <-time.After(healthPoll):
		}
	}
}

func allReady(health []engine.ComponentHealth) bool {
	if len(health) == 0 {
		return false // no components yet is suspicious, not success
	}
	for _, h := range health {
		if !h.Ready {
			return false
		}
	}
	return true
}

func unreadySummary(health []engine.ComponentHealth) string {
	if len(health) == 0 {
		return "no components reported yet"
	}
	var msgs []string
	for _, h := range health {
		if !h.Ready {
			msgs = append(msgs, fmt.Sprintf("%s: %s", h.Name, h.Message))
		}
	}
	return strings.Join(msgs, "; ")
}

// step prints one line of user-facing progress. It delegates to
// internal/ui, which reproduces this exact phase-1 format
// ("▸ [%s] %s\n") in plain mode and only ever renders styled output on a
// real terminal (never in tests, e2e, or CI — see ui.Resolve). ui.PlainFlag
// mirrors the --plain persistent flag set once by cmd/root.go's
// PersistentPreRunE, before any orchestrator runs.
func step(w io.Writer, stage, format string, args ...any) {
	ui.New(w, ui.PlainFlag).Step(stage, format, args...)
}
