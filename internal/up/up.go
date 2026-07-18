// Package up orchestrates `cube-idp up` (spec §4.3). It sequences the
// already-tested units and owns user-facing progress output. It has no
// logic of its own beyond ordering and timeouts — keep it that way.
package up

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cube-idp/cube-idp/internal/apply"
	"github.com/cube-idp/cube-idp/internal/bundle"
	"github.com/cube-idp/cube-idp/internal/cluster"
	"github.com/cube-idp/cube-idp/internal/config"
	"github.com/cube-idp/cube-idp/internal/diag"
	"github.com/cube-idp/cube-idp/internal/engine"
	enginefactory "github.com/cube-idp/cube-idp/internal/engine/factory"
	"github.com/cube-idp/cube-idp/internal/lock"
	"github.com/cube-idp/cube-idp/internal/oci"
	"github.com/cube-idp/cube-idp/internal/pack"
	"github.com/cube-idp/cube-idp/internal/registry"
	"github.com/cube-idp/cube-idp/internal/trust"
	"github.com/cube-idp/cube-idp/internal/ui"
	"github.com/cube-idp/cube-idp/internal/ui/event"
)

const dnsTimeout = 2 * time.Minute

// healthPoll and healthLogEvery are vars, not consts, so the waitHealthy
// narration test can shrink them — this package has no fake clock.
// Production never mutates them.
var (
	// healthPoll paces waitHealthy's Health polling.
	healthPoll = 5 * time.Second
	// healthLogEvery paces waitHealthy's StepLog narration (U1): while
	// components stay unhealthy, one "waiting on: <components>" line per
	// interval — live-mode-only richness, zero plain/JSON bytes.
	healthLogEvery = 15 * time.Second
)

const (
	clusterTimeout = 3 * time.Minute
	applyTimeout   = 2 * time.Minute
	healthTimeout  = 5 * time.Minute
	// The gateway pack delivers the Gateway API CRDs asynchronously (the
	// traefik pack ships them as static manifests, the envoy-gateway pack
	// installs them via its Helm-charted controller), so the registry
	// HTTPRoute apply must wait for them to be Established. Envoy's chart
	// pull + controller startup + CRD install is the slow path, so this
	// deadline is generous — but hard, per spec §4.5 (no infinite spinner).
	gatewayCRDTimeout = 3 * time.Minute
	gatewayCRDPoll    = 2 * time.Second
	// httpRouteCRD is the Gateway API CRD every gateway pack must establish
	// before the registry HTTPRoute (registry.GatewayRoute) can be applied.
	httpRouteCRD = "httproutes.gateway.networking.k8s.io"
)

// Options configures a single Run. ConfigPath is the cube.yaml to install;
// Con is the event sink cmd/up.go wires through ui.RunPipeline (Task 14b);
// Bundle, when non-empty, switches Run to fully-offline mode: every pack
// source is served from the bundle and every image is node-loaded from it,
// with any attempt to leave those rails a typed error (Task 7).
type Options struct {
	ConfigPath string      // path to cube.yaml
	Bundle     string      // path to a vendor bundle; "" = online mode
	Con        *ui.Console // progress/event sink (never nil)
}

// Run drives the full up sequence for the cube.yaml at opts.ConfigPath,
// emitting progress events through opts.Con (Task 14b: cmd/up.go wraps Run in
// ui.RunPipeline, which owns the renderer for the resolved mode): load
// config -> ensure the local CA (D12: before any cluster artifact
// references the trust root) -> ensure cluster -> install registry ->
// install engine -> ensure the gateway TLS secret -> port-forward the
// registry -> fetch/render/push/deliver every pack (gateway first) -> wait
// for engine-reported health -> emit a success summary.
//
// When opts.Bundle is set, exactly three deviations make the install offline
// (spec §4.1): the provider must satisfy cluster.ImageLoader or Run fails
// fast before any cluster mutation (CUBE-7005); every bundled image is
// node-loaded right after the cluster is ready and before anything installs;
// and every pack ref is rewritten to its bundle-local source dir before the
// pack loop, so no fetch ever touches the network.
func Run(ctx context.Context, opts Options) error {
	con := opts.Con
	cfgPath := opts.ConfigPath
	cube, err := config.Load(cfgPath)
	if err != nil {
		return err // no RunStarted: a failed load emits only RunDone+Diagnosis
	}
	con.Start("up", cube.Metadata.Name)
	con.Step("config", "cube %q loaded and validated", cube.Metadata.Name)

	// Offline mode (spec §4.1): open and verify the bundle up front so a
	// corrupt or incomplete bundle fails before any cluster artifact exists.
	var opened *bundle.Opened
	if opts.Bundle != "" {
		if opened, err = bundle.Open(opts.Bundle); err != nil {
			return err
		}
		defer opened.Close()
		if err := opened.Verify(); err != nil {
			return err
		}
		con.Step("bundle", "bundle verified — content hashes OK, %d packs / %d images present",
			len(opened.Lock.Packs), len(opened.Manifest.Images))
	}

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
	// U1: stream the provider's own provisioning narration (kind's
	// "Ensuring node image ..." etc., k3d's logrus lines) into the StepLog
	// event channel — the live renderer's dim log tail under the open
	// cluster step. Machine modes project StepLog as zero bytes (frozen
	// matrix), so this adds no plain/JSON output.
	if lg, ok := prov.(cluster.Loggable); ok {
		lg.SetLogSink(func(line string) { con.Log("cluster", "%s", line) })
	}
	// Deviation 1 (offline): refuse an un-loadable topology up front, before
	// any cluster mutation. `existing` cannot node-load images, so a bundle
	// install against it would silently fall back to registry pulls the
	// air-gapped host cannot reach — reject it with an actionable CUBE-7005
	// rather than fail deep in image-less pod startup later.
	if opened != nil {
		if _, ok := prov.(cluster.ImageLoader); !ok {
			return diag.New(diag.CodeBundleNoImageLoader,
				fmt.Sprintf("--bundle needs a provider that can load images into nodes; %q cannot", cube.Spec.Cluster.Provider),
				"use provider: kind or k3d for air-gapped installs, or pre-load the images into a registry your existing cluster can reach and run `up` without --bundle")
		}
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

	// Deviation 2 (offline): node-load every bundled image now — after the
	// cluster exists, before anything installs — so the engine, zot, and
	// every pack's pods start from node-local images with no registry pull.
	// The ImageLoader assertion already succeeded in deviation 1.
	if opened != nil {
		lp := con.Progress("bundle", "loading images into cluster nodes")
		if err := prov.(cluster.ImageLoader).LoadImages(ctx, cube.Metadata.Name, opened.ImageTars()); err != nil {
			lp.Stop()
			return err // LoadImages wraps with CUBE-7006 (or CUBE-7002 for a ListNodes failure) and names the failing image
		}
		lp.Done("%d image(s) loaded into cluster nodes", len(opened.ImageTars()))
	}

	a, err := apply.New(conn.REST, cube.Metadata.Name)
	if err != nil {
		return err
	}
	eng, err := enginefactory.New(cube.Spec.Engine)
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
	// Deviation 3 (offline): rewrite every ref to its bundle-local source dir
	// before the loop's pack.Fetch runs, so fetching reads from disk and a
	// ref absent from the bundle fails loudly (CUBE-7004) instead of falling
	// through to a network pull the air-gapped host cannot make.
	if opened != nil {
		refs, err = resolveBundleRefs(refs, opened.Lock, opened.PackDirLookup())
		if err != nil {
			return err
		}
	}
	var entries []lock.Entry
	var packs []*pack.Pack // kept in lockstep with entries: Task 12.5 needs each Pack's Expose after waitHealthy
	for i, pref := range refs {
		// Each pack delivery is an enumerated open step (pack i+1/len(refs))
		// so renderers can show n-of-m; the Done message is byte-identical to
		// the previous con.Step line and plain never prints Dur — zero plain
		// drift.
		if err := func() error {
			pr := con.ProgressN("pack", "delivering "+pref.Ref, i+1, len(refs))
			defer pr.Stop() // no-op after Done; resolves the step on any error return
			// Task 13 review: record the RESOLVED fetch source before Fetch runs.
			// This is the falsifiable output proof of offline honesty: an online
			// run prints the oci:// ref here; a --bundle run prints the
			// bundle-local dir (under cube-idp-bundle-*), never oci://. A new
			// additive plain line, consistent with the existing step conventions.
			stepFetchSource(con, pref.Ref)
			// pk, not p: kept distinct from pack.Pack's other short names used
			// elsewhere in this package (verifyGatewayPackRef's own pk param).
			pk, err := pack.Fetch(ctx, pref.Ref, dir)
			if err != nil {
				return err
			}
			// F11: refs[0] is the gateway pack (prepended above). Fail loudly if a
			// gateway.ref/gateway.pack mismatch means the ref would silently
			// deliver a different gateway than pack: names, before any cluster
			// mutation for this pack.
			if i == 0 {
				if err := verifyGatewayPackRef(pk, cube.Spec.Gateway); err != nil {
					return err
				}
			}
			packs = append(packs, pk)
			rendered, err := pk.RenderFor(pref.Values, cube.Spec.Gateway)
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
				Ref:          pref.Ref,
				Name:         rendered.Name,
				Version:      rendered.Version,
				Resolved:     pk.Pinned,
				RenderedHash: rh,
				// D14: union rendered-manifest images with the pack's own
				// declared images (pack.cue images:) — see the Entry.Images
				// field comment for why both sources matter.
				Images: mergeImages(lock.ImagesFrom(rendered.Objects), pk.Images),
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
			pr.Done("%s@%s delivered", rendered.Name, rendered.Version)
			return nil
		}(); err != nil {
			return err
		}
	}

	lf := &lock.File{APIVersion: "cube-idp.dev/v1alpha1", Kind: "CubeLock",
		Engine: lock.EngineLock{Type: cube.Spec.Engine.Type}, Packs: entries}
	if err := lock.Write(lock.PathFor(cfgPath), lf); err != nil {
		return err
	}
	con.Step("lock", "cube.lock written (%d packs)", len(entries))

	// The registry HTTPRoute (registry.GatewayRoute below) is applied after
	// the gateway pack is delivered, but pack delivery is asynchronous: the
	// engine reconciles the Gateway API CRDs on its own schedule. With
	// traefik the CRDs ship as static manifests and reconcile early enough to
	// win the race; with envoy-gateway they arrive via a Helm-charted
	// controller and lag behind, so a server-side apply of the HTTPRoute
	// races ahead and dry-run fails with "no matches for kind HTTPRoute"
	// (CUBE-2003). Provider-agnostically block until the CRD is Established
	// first — a no-op wait when it already is (the traefik path).
	if err := waitCRDEstablished(ctx, a, con, httpRouteCRD, gatewayCRDTimeout); err != nil {
		return err
	}

	// D6 canonical hostname: route registry.<host> through the gateway (for
	// host-side docker/oras push), then make *.<host> resolve in-cluster to
	// the same gateway Service so pod-side clients use identical URLs.
	route := registry.GatewayRoute(cube.Spec.Gateway.Host, cube.Spec.Gateway.Pack)
	if err := a.Apply(ctx, []*unstructured.Unstructured{route}, false, applyTimeout); err != nil {
		return err
	}
	if err := a.RecordInventory(ctx, []*unstructured.Unstructured{route}); err != nil {
		return err
	}
	// packs[0] is always the gateway pack (prepended before the loop above),
	// and the loop always runs at least once (refs has at least the gateway
	// entry), so packs is never empty here.
	if err := trust.EnsureCoreDNSRewrite(ctx, a.Client(), cube.Spec.Gateway.Host,
		gatewayServiceFQDN(cube.Spec.Gateway, packs[0]), dnsTimeout); err != nil {
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
	// The epilogue is DATA (event.Epilogue, TE-4): the ✔ glyph is
	// presentation renderers add, so the plain bytes drop exactly that one
	// glyph (ratified R2, design doc §5).
	con.Epilogue(event.Epilogue{
		Cube:       cube.Metadata.Name,
		GatewayURL: fmt.Sprintf("https://%s:%d", cube.Spec.Gateway.Host, cube.Spec.Gateway.Port),
		Context:    conn.Context,
		Registry:   registry.InClusterURL,
		Hint:       "credentials: cube-idp get secrets",
	})

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

// stepFetchSource emits the per-pack resolved-fetch-source step line —
// "fetching <source>" where source is exactly what pack.Fetch is about to
// read: the oci://... (or local/git) ref online, or the bundle-local staging
// dir (under a cube-idp-bundle-* temp dir) after resolveBundleRefs in
// --bundle mode. Added by the Task 13 review so offline honesty is
// falsifiable from output alone: the e2e bundle test asserts every fetch
// source points into the bundle and none is an oci:// ref — assertions that
// would FAIL on an online run, because this line demonstrably prints the
// network ref there (pinned by TestStepFetchSourcePlainOutput).
func stepFetchSource(con *ui.Console, ref string) {
	con.Step("pack", "fetching %s", ref)
}

// mergeImages returns the sorted, deduplicated union of rendered (images
// found by walking a pack's rendered manifests) and declared (pack.cue's
// optional images: list, spec D14) — the Entry.Images the lock records.
// Operator-style packs (e.g. envoy-gateway) provision images that never
// appear in their own rendered objects, so `declared` closes that air-gap
// blind spot for `cube-idp vendor` (Task 6).
func mergeImages(rendered, declared []string) []string {
	set := make(map[string]struct{}, len(rendered)+len(declared))
	for _, img := range rendered {
		set[img] = struct{}{}
	}
	for _, img := range declared {
		set[img] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for img := range set {
		out = append(out, img)
	}
	sort.Strings(out)
	return out
}

// gatewayServiceFQDN returns the in-cluster DNS name of the gateway pack's
// data-plane Service, the CoreDNS rewrite target for *.<gw.Host> (D6, R7b
// spec §5.7b). When the RESOLVED gateway pack (gwPack) declares a
// gatewayService: block, that is authoritative — this is how envoy-gateway
// closes the CoreDNS-targets-the-controller gap (the pre-R7b KNOWN GAP):
// its controller Service and its data-plane Service are different Services,
// and only gatewayService: names the latter. Otherwise (gwPack nil, or no
// declaration — every pack before R7b, including traefik) falls back to the
// <pack>.<pack>.svc.cluster.local convention: traefik's chart installs
// releaseName "traefik" into namespace "traefik" (packs/traefik/chart.yaml),
// so gw.Pack doubles as both name and namespace there — zero migration.
func gatewayServiceFQDN(gw config.GatewaySpec, gwPack *pack.Pack) string {
	if gwPack != nil && gwPack.GatewayService != nil {
		return fmt.Sprintf("%s.%s.svc.cluster.local", gwPack.GatewayService.Name, gwPack.GatewayService.Namespace)
	}
	return fmt.Sprintf("%s.%s.svc.cluster.local", gw.Pack, gw.Pack)
}

// verifyGatewayPackRef guards F11: gateway.ref silently wins over
// gateway.pack (GatewaySpec.PackRef), so an operator who edits only
// `pack: envoy-gateway` while `init --local` left `ref: .../packs/traefik`
// in place gets traefik delivered under an envoy-gateway label with no
// error. When BOTH are set, the fetched gateway pack's declared pack.cue
// name must equal gw.Pack, else the two disagree about which gateway this
// is. A bare gw.Ref == "" (PackRef falls back to packs/<Pack>) or gw.Pack
// == "" cannot disagree, so those skip the check. The seam is here, not
// config.Load: Load is pure YAML+CUE with no pack-dir I/O, whereas `up`
// already fetches the gateway pack (pk) and thus holds its parsed pack.cue.
func verifyGatewayPackRef(pk *pack.Pack, gw config.GatewaySpec) error {
	if gw.Ref == "" || gw.Pack == "" || pk.Name == gw.Pack {
		return nil
	}
	return diag.New(diag.CodeGatewayPackMismatch,
		fmt.Sprintf("spec.gateway.ref resolves to the %q pack, but spec.gateway.pack is %q — the ref silently wins, so cube-idp would deliver %q", pk.Name, gw.Pack, pk.Name),
		fmt.Sprintf("update spec.gateway.ref to the %s pack or remove it to use the published ref", gw.Pack))
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
	lastLog := time.Now()
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
		// U1: narrate the silent stretch — every healthLogEvery of
		// unhealthiness, one StepLog line naming what the wait is on.
		// Live-only richness (dim log tail); plain and JSON project StepLog
		// as zero bytes per the frozen mode matrix. Checked before the
		// deadline so even a timing-out wait narrates its last state.
		// Stage "health" (U2 Step 0): the live tail follows the OPEN step's
		// stage, and the step open here is the "health" Progress above — a
		// mismatched stage would emit but never display.
		if time.Since(lastLog) >= healthLogEvery {
			waitingOn := strings.Join(notReadyNames(health), ", ")
			if waitingOn == "" {
				waitingOn = "no components reported yet"
			}
			con.Log("health", "waiting on: %s", waitingOn)
			lastLog = time.Now()
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

// waitCRDEstablished blocks until the named CustomResourceDefinition reports
// its Established condition true (the API server serves its kind) or timeout
// elapses. It mirrors the 'Pack CRD established' wait (a.Apply(..., true))
// and waitHealthy: a Console progress line spans the wait, and — per spec
// §4.5, no infinite spinner — a hard deadline renders a typed CUBE-5005 with
// remediation rather than hanging. It is provider- and pack-agnostic: when
// the CRD is already Established (the traefik path, whose gateway pack ships
// the CRDs as static manifests) the first poll returns immediately; the wait
// only bites when the CRD lags behind, as with the envoy-gateway chart.
func waitCRDEstablished(ctx context.Context, a *apply.Applier, con *ui.Console, crdName string, timeout time.Duration) error {
	pr := con.Progress("gateway-crd", "waiting for the Gateway API HTTPRoute CRD")
	deadline := time.Now().Add(timeout)
	for {
		var crd apiextensionsv1.CustomResourceDefinition
		err := a.Client().Get(ctx, client.ObjectKey{Name: crdName}, &crd)
		if err == nil && crdEstablished(&crd) {
			pr.Done("Gateway API HTTPRoute CRD established")
			return nil
		}
		if time.Now().After(deadline) {
			pr.Stop()
			return diag.New(diag.CodeRegistryRouteCRDTimeout,
				fmt.Sprintf("timed out after %s waiting for the %s CRD to be Established before applying the registry HTTPRoute", timeout, crdName),
				"the gateway pack installs the Gateway API CRDs — inspect the gateway pack's components with kubectl, then re-run `cube-idp up` (idempotent)")
		}
		select {
		case <-ctx.Done():
			pr.Stop()
			return diag.Wrap(ctx.Err(), diag.CodeRegistryRouteCRDTimeout, "Gateway API CRD wait aborted before completion",
				"re-run `cube-idp up` — it is idempotent and resumes where it left off")
		case <-time.After(gatewayCRDPoll):
		}
	}
}

// crdEstablished reports whether the CRD's Established condition is true — the
// signal that the API server has registered the kind and will serve (and
// dry-run apply) its objects.
func crdEstablished(crd *apiextensionsv1.CustomResourceDefinition) bool {
	for _, c := range crd.Status.Conditions {
		if c.Type == apiextensionsv1.Established {
			return c.Status == apiextensionsv1.ConditionTrue
		}
	}
	return false
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

// notReadyNames lists the names of the not-ready components — the payload
// of waitHealthy's U1 "waiting on: ..." narration (unreadySummary is its
// name+message sibling for the terminal CUBE-3004).
func notReadyNames(health []engine.ComponentHealth) []string {
	var names []string
	for _, h := range health {
		if !h.Ready {
			names = append(names, h.Name)
		}
	}
	return names
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
