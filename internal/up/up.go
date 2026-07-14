// Package up orchestrates `cube-idp up` (spec §4.3). It sequences the
// already-tested units and owns user-facing progress output. It has no
// logic of its own beyond ordering and timeouts — keep it that way.
package up

import (
	"context"
	"fmt"
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
	"github.com/rafpe/cube-idp/internal/ui/event"
)

const dnsTimeout = 2 * time.Minute

const (
	clusterTimeout = 3 * time.Minute
	applyTimeout   = 2 * time.Minute
	healthTimeout  = 5 * time.Minute
	healthPoll     = 5 * time.Second
)

// Run drives the full up sequence for the cube.yaml at cfgPath, emitting
// progress events through con (Task 14b: cmd/up.go wraps Run in
// ui.RunPipeline, which owns the renderer for the resolved mode): load
// config -> ensure the local CA (D12: before any cluster artifact
// references the trust root) -> ensure cluster -> install registry ->
// install engine -> ensure the gateway TLS secret -> port-forward the
// registry -> fetch/render/push/deliver every pack (gateway first) -> wait
// for engine-reported health -> emit a success summary.
func Run(ctx context.Context, cfgPath string, con *ui.Console) error {
	cube, err := config.Load(cfgPath)
	if err != nil {
		return err // no RunStarted: a failed load emits only RunDone+Diagnosis
	}
	con.Start("up", cube.Metadata.Name)
	con.Step("config", "cube %q loaded and validated", cube.Metadata.Name)

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
	con.Step("ca", "local CA ready (%s)", ca.CertPath)

	prov, err := cluster.New(cube.Spec.Cluster, cube.Spec.Gateway)
	if err != nil {
		return err
	}
	// Task 15.3a: cluster creation can take minutes with zero prior output —
	// pr.Stop() on error prints nothing (matching the phase-1 behavior of
	// printing nothing when a step failed); pr.Done prints the same
	// "cluster" step line step() always printed on success.
	pr := con.Progress("cluster", fmt.Sprintf("creating %s cluster", cube.Spec.Cluster.Provider))
	clusterCtx, cancel := context.WithTimeout(ctx, clusterTimeout)
	conn, err := prov.Ensure(clusterCtx, cube.Metadata.Name, cube.Spec.Cluster)
	cancel()
	if err != nil {
		pr.Stop()
		return err
	}
	pr.Done("%s cluster ready (context %s)", cube.Spec.Cluster.Provider, conn.Context)

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
	con.Step("registry", "zot ready at %s", registry.InClusterURL)

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
	con.Step("packs-crd", "Pack CRD established")

	// Task 15.3a: the engine install (Flux/Argo CD's own controllers coming
	// up) is the second long, previously-silent wait.
	pr = con.Progress("engine", fmt.Sprintf("installing %s", cube.Spec.Engine.Type))
	if err := eng.Install(ctx, a, applyTimeout); err != nil {
		pr.Stop()
		return err
	}
	installObjs, err := eng.InstallManifests()
	if err != nil {
		pr.Stop()
		return err
	}
	if err := a.RecordInventory(ctx, installObjs); err != nil {
		pr.Stop()
		return err
	}
	pr.Done("%s installed", cube.Spec.Engine.Type)

	// The gateway pack's websecure listener references this secret by name
	// (packs/traefik/manifests/10-gateway.yaml); it must exist before the
	// engine reconciles the Gateway, so this runs before the pack loop.
	if err := ensureGatewayTLS(ctx, a, cube.Spec.Gateway); err != nil {
		return err
	}
	con.Step("tls", "gateway certificate ready (CA: run `cube-idp trust` to make browsers trust it)")

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
		// pk (not p): p is this function's *ui.Printer — shadowing it with a
		// same-named *pack.Pack here would still compile (the shadow is
		// scoped to this loop body), but pk keeps the two unambiguous.
		pk, err := pack.Fetch(ctx, pr.Ref, dir)
		if err != nil {
			return err
		}
		packs = append(packs, pk)
		rendered, err := pk.Render(pr.Values)
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
			Resolved:     pk.Pinned,
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
		con.Step("pack", "%s@%s delivered", rendered.Name, rendered.Version)
	}

	lf := &lock.File{APIVersion: "cube-idp.dev/v1alpha1", Kind: "CubeLock",
		Engine: lock.EngineLock{Type: cube.Spec.Engine.Type}, Packs: entries}
	if err := lock.Write(lock.PathFor(cfgPath), lf); err != nil {
		return err
	}
	con.Step("lock", "cube.lock written (%d packs)", len(entries))

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
	con.Step("dns", "*.%s resolves to the gateway in-cluster", cube.Spec.Gateway.Host)

	if err := waitHealthy(ctx, eng, a, con, healthTimeout); err != nil {
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
	for _, pk := range packs {
		packObjs = append(packObjs, pack.PackObject(pk, cube.Spec.Gateway, healthByName["cube-idp-"+pk.Name]))
	}
	if err := a.Apply(ctx, packObjs, false, applyTimeout); err != nil {
		return err
	}
	if err := a.RecordInventory(ctx, packObjs); err != nil {
		return err
	}
	con.Step("packs", "%d pack records written — try `kubectl get packs`", len(packObjs))

	// Phase 2: the gateway's websecure listener terminates TLS with a
	// CA-issued cert (D6/D12), so this URL is genuinely HTTPS. Browsers only
	// show a green lock once the CA is trusted — `cube-idp trust` does that.
	// The Note projection adds exactly one trailing newline, so the plain
	// bytes are unchanged from before Task 15.3 (the format string drops the
	// old trailing \n on purpose).
	con.Note("\n✔ cube %q is up — https://%s:%d\n  credentials: cube-idp get secrets",
		cube.Metadata.Name, cube.Spec.Gateway.Host, cube.Spec.Gateway.Port)

	// The "what did I just get" access summary — every delivered pack's
	// expose URLs (reusing pack.ExposeURLs, the same ${GATEWAY_HOST}
	// substitution PackObject's spec.url/spec.urls used above) plus the
	// get-secrets hint. Since Task 14b (Owner Decision #15, design doc §9)
	// Access is DATA with a stable plain projection — the one deliberate
	// plain-output addition: a "\nAccess\n" block scripts and CI can scrape.
	access := make([]ui.PackAccess, 0, len(packs))
	for _, pk := range packs {
		if urls := pack.ExposeURLs(pk, cube.Spec.Gateway); len(urls) > 0 {
			access = append(access, ui.PackAccess{Name: pk.Name, URLs: urls})
		}
	}
	con.Access(access, "credentials: cube-idp get secrets")
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
func waitHealthy(ctx context.Context, eng engine.Engine, a *apply.Applier, con *ui.Console, timeout time.Duration) error {
	// Task 15.3a: the third long, previously-silent wait — health polling
	// can run for minutes while packs converge. pr spans the whole poll
	// loop; every error/timeout return below is unchanged from before
	// (nothing printed in plain mode), so pr.Stop() keeps that contract.
	// Each poll additionally emits a change-filtered HealthTick (Task 14b)
	// so the live renderer's component table and the JSON stream see every
	// state transition — zero plain bytes, as before.
	pr := con.Progress("health", "waiting for components to become ready")
	deadline := time.Now().Add(timeout)
	for {
		health, err := eng.Health(ctx, a)
		if err != nil {
			pr.Stop()
			return err
		}
		con.Health(componentStates(health))
		if allReady(health) {
			pr.Done("%d component(s) ready", len(health))
			return nil
		}
		if time.Now().After(deadline) {
			pr.Stop()
			return diag.New(diag.CodeEngineHealthTimeout,
				fmt.Sprintf("timed out after %s waiting for components to become healthy: %s",
					timeout, unreadySummary(health)),
				"re-run `cube-idp up` (idempotent); inspect the listed components with kubectl")
		}
		select {
		case <-ctx.Done():
			pr.Stop()
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

// componentStates mirrors engine.ComponentHealth into the event package's
// dependency-light ComponentState (event never imports internal/engine).
func componentStates(health []engine.ComponentHealth) []event.ComponentState {
	states := make([]event.ComponentState, len(health))
	for i, h := range health {
		states[i] = event.ComponentState{Name: h.Name, Ready: h.Ready, Message: h.Message}
	}
	return states
}
