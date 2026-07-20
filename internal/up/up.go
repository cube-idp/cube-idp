// Package up orchestrates `cube-idp up`. It sequences the
// already-tested units and owns user-facing progress output. It has no
// logic of its own beyond ordering and timeouts — keep it that way.
package up

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
	sigyaml "sigs.k8s.io/yaml"

	"github.com/cube-idp/cube-idp/internal/apply"
	"github.com/cube-idp/cube-idp/internal/bundle"
	"github.com/cube-idp/cube-idp/internal/cfgload"
	"github.com/cube-idp/cube-idp/internal/cluster"
	"github.com/cube-idp/cube-idp/internal/config"
	"github.com/cube-idp/cube-idp/internal/diag"
	"github.com/cube-idp/cube-idp/internal/engine"
	enginefactory "github.com/cube-idp/cube-idp/internal/engine/factory"
	"github.com/cube-idp/cube-idp/internal/gitea"
	"github.com/cube-idp/cube-idp/internal/kube"
	"github.com/cube-idp/cube-idp/internal/lock"
	"github.com/cube-idp/cube-idp/internal/oci"
	"github.com/cube-idp/cube-idp/internal/pack"
	"github.com/cube-idp/cube-idp/internal/refval"
	"github.com/cube-idp/cube-idp/internal/registry"
	"github.com/cube-idp/cube-idp/internal/spoke"
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
	// deadline is generous — but hard: every wait ends in a bounded, typed
	// diagnosis rather than an infinite spinner.
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
// with any attempt to leave those rails a typed error.
type Options struct {
	ConfigPath string      // path to cube.yaml
	Bundle     string      // path to a vendor bundle; "" = online mode
	Con        *ui.Console // progress/event sink (never nil)
}

// Run drives the full up sequence for the cube.yaml at opts.ConfigPath,
// emitting progress events through opts.Con (Task 14b: cmd/up.go wraps Run in
// ui.RunPipeline, which owns the renderer for the resolved mode): load
// config -> ensure the local CA (before any cluster artifact
// references the trust root) -> ensure cluster -> install registry ->
// install engine -> ensure the gateway TLS secret -> port-forward the
// registry -> fetch/render/push/deliver every pack (gateway first) -> wait
// for engine-reported health -> emit a success summary.
//
// When opts.Bundle is set, exactly three deviations make the install offline
// the install offline: the provider must satisfy cluster.ImageLoader or Run fails
// fast before any cluster mutation (CUBE-7005); every bundled image is
// node-loaded right after the cluster is ready and before anything installs;
// and every pack ref is rewritten to its bundle-local source dir before the
// pack loop, so no fetch ever touches the network.
func Run(ctx context.Context, opts Options) error {
	con := opts.Con
	cfgPath := opts.ConfigPath
	cube, err := cfgload.Load(ctx, cfgPath)
	if err != nil {
		return err // no RunStarted: a failed load emits only RunDone+Diagnosis
	}
	con.Start("up", cube.Metadata.Name)
	con.Step("config", "cube %q loaded and validated", cube.Metadata.Name)
	// Remote -f provenance: a tag ref is not reproducible, so at
	// minimum make the ref and the pin it resolved to visible in the log.
	if o := cube.Origin(); o.Remote {
		con.Step("config", "using remote config %s (%s)", o.Ref, o.Pin)
	}

	// Offline mode: open and verify the bundle up front so a
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
		if err := bundleRailsCheck(cube); err != nil {
			return err
		}
	}

	// Cert material is generated before cluster creation
	// (docs/adr/0038-local-ca-and-tls-at-the-gateway.md): ensure the
	// local CA — adopting an existing mkcert root if present — before
	// ClusterProvider.Ensure runs, so the kind provider can mount it into
	// containerd certs.d at cluster-create time and no cluster
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
	// Cluster creation can take minutes with zero prior output, so it runs
	// behind a progress handle rather than a bare step line —
	// pr.Stop() on error prints nothing (matching the older behavior of
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

	// The inert Pack CRD must exist before any Pack record is written,
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

	// The engine install (Flux/Argo CD's own controllers coming
	// up) is the second long, previously-silent wait.
	// Self-management contract: the install is ALWAYS rendered first (the rendered engine
	// pack, values applied) — the rendered objects are what SSA applies, what
	// the inventory records, and (selfManage) what the cube-engine artifact
	// carries, so the SSA'd state and the first pushed artifact are
	// byte-identical renders. SSA is skipped only when the engine owns
	// itself: selfManage on AND healthy at start (rule 2); first install
	// (rule 1) and unhealthy-at-start recovery (rule 3) SSA directly, and
	// selfManage off keeps the original always-SSA behavior.
	dir, err := pack.DefaultCacheDir()
	if err != nil {
		return err
	}
	// Engine-as-pack (ADR-0007): the engine install is fetched and rendered
	// like any pack — the rendered objects are what SSA applies, what the
	// inventory records, and (selfManage) what the cube-engine artifact
	// carries. Offline: the ref resolves through the bundle like every pack.
	engineRef := cube.Spec.Engine.PackRef()
	if opened != nil {
		eref, err := resolveBundleRefs([]config.PackRef{{Ref: engineRef}}, opened.Lock, opened.PackDirLookup())
		if err != nil {
			return err
		}
		engineRef = eref[0].Ref
	}
	epr := con.Progress("engine-pack", "fetching "+engineRef)
	stepFetchSource(con, engineRef)
	enginePk, engineRendered, err := pack.FetchRenderEngine(ctx, cube.Spec.Engine, cube.Spec.Gateway, engineRef, dir)
	if err != nil {
		epr.Stop()
		return err
	}
	epr.Done("%s@%s rendered", engineRendered.Name, engineRendered.Version)
	pr = con.Progress("engine", fmt.Sprintf("installing %s", cube.Spec.Engine.Type))
	installObjs := engineRendered.Objects
	ssaEngine := installNeedsSSA(ctx, eng, a, cube.Spec.Engine.SelfManage)
	if ssaEngine {
		if err := a.Apply(ctx, installObjs, true, applyTimeout); err != nil {
			pr.Stop()
			return err
		}
	}
	if err := a.RecordInventory(ctx, installObjs); err != nil {
		pr.Stop()
		return err
	}
	if ssaEngine {
		pr.Done("%s installed", cube.Spec.Engine.Type)
	} else {
		pr.Done("%s healthy — self-managed, install SSA skipped", cube.Spec.Engine.Type)
	}

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

	// Gateway pack goes first — everything else depends on ingress existing.
	// The gitea guarantee: with any delivery: repo pack
	// declared, gitea is delivered before it — since p6 DEP2 this is
	// pack.ResolveOrder's implicit repo->gitea edge (resolveAndDeliverPacks'
	// graph pass below), not orderPackRefs; either way the repo-delivery
	// readiness gate below waits on a pack that is already reconciling.
	refs := orderPackRefs(cube.Spec.Gateway.PackRef(), cube.Spec.Packs)
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
	// The per-pack delivery collaborators, faked in the branch unit
	// tests. The gitea session is lazy — established once, at the first
	// delivery: repo pack (which the ordering above put right after gitea
	// itself was delivered), then shared; its port-forward closes with Run.
	var giteaStop func()
	defer func() {
		if giteaStop != nil {
			giteaStop()
		}
	}()
	var giteaCli giteaPacks
	deps := deliverDeps{
		eng:        eng,
		applier:    a,
		tunnelAddr: tunnelAddr,
		pushOCI:    oci.PushRendered,
		gitea: func(ctx context.Context) (giteaPacks, error) {
			if giteaCli != nil {
				return giteaCli, nil
			}
			g, stop, err := giteaSession(ctx, applyTimeout, giteaReadyPoll,
				func(ctx context.Context) (giteaPacks, func(), error) {
					return giteaConnectOnce(ctx, conn.REST, a.Client())
				})
			if err != nil {
				return nil, err
			}
			giteaCli, giteaStop = g, stop
			return giteaCli, nil
		},
	}

	// Pass 1: fetch + render every pack in DECLARED order. No cluster
	// mutation happens here beyond what pack.Fetch itself may cache to disk —
	// entries/packs/renders are index-aligned with refs, one append per ref,
	// so a failure partway through leaves no partial delivery behind.
	var entries []lock.Entry
	var packs []*pack.Pack // kept in lockstep with entries: the Pack discoverability records need each Pack's Expose after waitHealthy
	var renders []*pack.Rendered
	for i, pref := range refs {
		if err := func() error {
			pr := con.ProgressN("pack-fetch", "fetching "+pref.Ref, i+1, len(refs))
			defer pr.Stop() // no-op after Done; resolves the step on any error return
			// Record the RESOLVED fetch source before Fetch runs.
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
			// refs[0] is the gateway pack (prepended above). Fail loudly if a
			// gateway.ref/gateway.pack mismatch means the ref would silently
			// deliver a different gateway than pack: names, before any cluster
			// mutation for this pack.
			if i == 0 {
				if err := verifyGatewayPackRef(pk, cube.Spec.Gateway); err != nil {
					return err
				}
			}
			packs = append(packs, pk)
			// RenderResolved is RenderWith plus the values rule extended to
			// valuesRef — the remote base fetch (CUBE-4021) and the RFC 7386
			// inline-over-fetched merge run first, then RenderWith enforces
			// the rule itself (values or valuesRef on a chartless pack is
			// CUBE-4016) and substitutes + appends extraManifests
			// (CUBE-4017). Inline-only packs pass straight through, unchanged.
			rendered, valuesPin, err := pack.RenderResolved(ctx, pk, pref, cube.Spec.Gateway, dir)
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
				ValuesRef:    pref.ValuesRef,
				ValuesPin:    valuesPin,
				// Union rendered-manifest images with the pack's own
				// declared images (pack.cue images:) — see the Entry.Images
				// field comment for why both sources matter.
				Images: mergeImages(lock.ImagesFrom(rendered.Objects), pk.Images),
			})
			renders = append(renders, rendered)
			pr.Done("%s@%s rendered", rendered.Name, rendered.Version)
			return nil
		}(); err != nil {
			return err
		}
	}

	// Passes 2+3: resolve the dependency graph, then deliver in that order.
	// Split out so the fail-fast property (a graph error returns before any
	// deliverPack call) and the topo delivery order are unit-testable with
	// the delivery fakes, without a live cluster. resolveAndDeliverPacks threads
	// each pack's resolved deps into its Rendered/engine call and the wave
	// gate itself (p6 DEP3); Run keeps the packDeps return too, now that the
	// Pack-record writer loop below needs each pack's resolved dep list for
	// its DEPENDS-ON column (p6 DEP4).
	packDeps, err := resolveAndDeliverPacks(ctx, con, deps, a, refs, packs, renders)
	if err != nil {
		return err
	}

	engRH, err := lock.RenderedHash(engineRendered.Objects)
	if err != nil {
		return err
	}
	// Cluster providerConfig pin (spec 2026-07-19 §6): re-resolve the ref the
	// cluster ensure already fetched (so this is a cache hit) purely to
	// surface its pin, instead of plumbing a pin return through the whole
	// provider interface. Best-effort by design: the cluster is already up
	// from this exact ref, so a transient re-resolve failure must not fail
	// the whole `up` — it only leaves the lock's cluster section absent. Say so
	// out loud, though: an absent cluster section makes the next
	// `upgrade --plan` report the providerConfigRef as "new (not in cube.lock)".
	var clusterLock *lock.ClusterLock
	if ref := cube.Spec.Cluster.ProviderConfigRef; ref != "" {
		if _, pin, err := refval.Resolve(ctx, ref, dir); err == nil {
			clusterLock = &lock.ClusterLock{ProviderConfigRef: ref, ProviderConfigPin: pin}
		} else {
			con.Note("warning: could not pin providerConfigRef %s (%v); cube.lock records no cluster section, so `cube-idp upgrade --plan` will report it as new", ref, err)
		}
	}
	lf := &lock.File{APIVersion: "cube-idp.dev/v1alpha1", Kind: "CubeLock",
		Engine: lock.EngineLock{Type: cube.Spec.Engine.Type, Ref: cube.Spec.Engine.PackRef(),
			Name: engineRendered.Name, Version: engineRendered.Version, Resolved: enginePk.Pinned,
			RenderedHash: engRH,
			Images:       mergeImages(lock.ImagesFrom(engineRendered.Objects), enginePk.Images)},
		Cluster: clusterLock,
		Packs:   entries}
	if err := lock.Write(lock.PathForOrigin(cfgPath, cube.Origin().Remote), lf); err != nil {
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

	// Canonical hostname (docs/adr/0012-canonical-gateway-host-and-port-mapping.md):
	// route registry.<host> through the gateway (for
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

	// Self-management: with selfManage on, hand the engine its own (tuned)
	// render as a zot artifact and attach the engine-native self-source —
	// from here the engine reconciles itself, and later `up`s render → push
	// → poke without ever SSA-ing a healthy engine. Runs after the health
	// gate so the first attach binds to a healthy engine; the re-wait below
	// is instant when artifact == live state (the first enable pushes the
	// byte-identical render SSA just applied — no restart, no flap).
	if cube.Spec.Engine.SelfManage {
		spr := con.Progress("engine-self", "handing the engine its own install (selfManage)")
		// The pre-pack-loop registry tunnel (tunnelAddr) is minutes old by
		// now — the CRD wait, the CoreDNS restart, and the health
		// convergence all ran since — and a client-go port-forward that old
		// can be dead (its local listener closes once the SPDY session
		// drops; a live run caught exactly that as a connection-refused
		// push). The push therefore gets a fresh bounded tunnel of its own,
		// acquired at use time like the gitea session is.
		selfAddr, selfStop, err := registry.PortForward(ctx, conn.REST)
		if err != nil {
			spr.Stop()
			return diag.Wrap(err, diag.CodeEngineSelfManage,
				"engine self-management: cannot open a registry tunnel for the cube-engine push",
				"re-run `cube-idp up` — it is idempotent and resumes where it left off")
		}
		selfDeps := deps
		selfDeps.tunnelAddr = selfAddr
		err = deliverEngineSelf(ctx, selfDeps, installObjs)
		selfStop()
		if err != nil {
			spr.Stop()
			return err
		}
		spr.Done("engine self-managed from oci://%s/packs/%s:%s", registry.InClusterURL, engine.SelfArtifactName, engineSelfTag)
		if err := waitHealthy(ctx, eng, a, con, healthTimeout); err != nil {
			return diag.Wrap(err, diag.CodeEngineSelfManage,
				"engine did not settle after the self-source attach",
				"re-run `cube-idp up` — it is idempotent and resumes where it left off")
		}
	}

	// Write each pack's discoverability record now that health is
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
	// Pack-record writer fields (append-only shared surface): U4 CUSTOMIZED,
	// DELIVERY (PackObject maps an empty Delivery to "oci"), and
	// DEP4 DEPENDS-ON (packDeps is keyed by name, packs is the DECLARED-order
	// slice — no index remap needed against refs/packs alignment below).
	// packs is index-aligned with refs (exactly one append per ref in the
	// delivery loop above, any failure aborts Run), so refs[i] is the
	// PackRef whose values/extraManifests/delivery decide packs[i]'s
	// CUSTOMIZED and DELIVERY columns.
	packObjs := make([]*unstructured.Unstructured, 0, len(packs))
	for i, pk := range packs {
		// RV2: a remotely-valued pack IS customized — valuesRef carries the
		// same "not the pack author's stock render" meaning as inline values.
		customized := len(refs[i].Values) > 0 || refs[i].ValuesRef != "" || refs[i].ExtraManifests != ""
		packObjs = append(packObjs, pack.PackObject(pk, cube.Spec.Gateway, healthByName["cube-idp-"+pk.Name], customized, refs[i].Delivery, packDeps[pk.Name]))
	}
	// Engine-as-pack §3.3.7: the engine's own row. READY is true by
	// construction here (waitHealthy gated above) unless selfManage, where
	// the cube-engine self-source's component health is the honest answer.
	engineReady := true
	if cube.Spec.Engine.SelfManage {
		engineReady = healthByName[engine.SelfArtifactName]
	}
	packObjs = append(packObjs, pack.PackObject(enginePk, cube.Spec.Gateway, engineReady,
		len(cube.Spec.Engine.Values) > 0, "engine", nil))
	if err := a.Apply(ctx, packObjs, false, applyTimeout); err != nil {
		return err
	}
	if err := a.RecordInventory(ctx, packObjs); err != nil {
		return err
	}
	con.Step("packs", "%d pack records written — try `kubectl get packs`", len(packObjs))

	// Spokes (ADR-0013): bootstrap and register, then the engine
	// takes over. Failure of one spoke aborts up (fail loud, spec thesis);
	// re-running up is the retry path and re-issues the spoke's TokenRequest
	// credential (servers may clamp its TTL, so every up refreshes it).
	for i, sp := range cube.Spec.Spokes {
		spr := con.ProgressN("spoke", fmt.Sprintf("spoke %q (%s)", sp.Name, sp.Cluster.Provider), i+1, len(cube.Spec.Spokes))
		if err := ensureSpoke(ctx, cube, sp, a, con); err != nil {
			spr.Stop()
			return err
		}
		spr.Done("spoke %q registered with %s", sp.Name, cube.Spec.Engine.Type)
	}

	// The gateway's websecure listener terminates TLS with a
	// CA-issued cert (ADR-0038), so this URL is genuinely HTTPS. Browsers only
	// show a green lock once the CA is trusted — `cube-idp trust` does that.
	// The epilogue is DATA (event.Epilogue): the ✔ glyph is
	// presentation renderers add, so the plain bytes drop exactly that one
	// glyph — renderers own glyphs, event content never carries them
	// (ADR-0025).
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
	// get-secrets hint. Since the move to the typed event stream,
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

// bundleRailsCheck enforces offline honesty for --bundle runs (CUBE-7007):
// the bundle vendors pack refs and images, NOT remote values/config sources —
// any of those alongside a bundle would either touch the network or fail
// mid-install, so refuse before any cluster mutation (the CUBE-7005
// fail-fast precedent). Pure and unit-testable: no bundle, no cluster.
func bundleRailsCheck(cube *config.Cube) error {
	if o := cube.Origin(); o.Remote {
		return diag.New(diag.CodeBundleRemoteSource,
			fmt.Sprintf("config was loaded from remote ref %q — remote configs are not vendored into the bundle", o.Ref),
			"fetch the cube.yaml locally and pass the local path to -f for air-gapped installs")
	}
	for _, p := range cube.Spec.Packs {
		if p.ValuesRef != "" {
			return diag.New(diag.CodeBundleRemoteSource,
				fmt.Sprintf("pack %q has valuesRef %q — remote values are not vendored into the bundle", p.Ref, p.ValuesRef),
				"inline the values (remove valuesRef) for air-gapped installs, or run without --bundle")
		}
	}
	return nil
}

// resolveAndDeliverPacks is Run's passes 2+3 (p6 DEP2): resolve the
// dependency graph over the already fetched+rendered packs (index-aligned
// with refs/packs/renders, exactly Run's pass-1 output), then deliver each
// one — via deliverPack, so the OCI/repo delivery branch is unchanged — in the
// resolved topo order rather than declared order. A graph error (CUBE-4018
// unknown dep, CUBE-4019 cycle, CUBE-4020 gateway carries a dep) returns
// BEFORE the delivery loop runs at all: the fail-fast improvement over the
// old single fetch+render+deliver loop, where a graph problem could only be
// detected pack-by-pack, after every prior pack in declared order had
// already been delivered to the cluster. Extracted from Run so this
// ordering/fail-fast contract is unit-testable with the delivery fakes, without a
// live cluster (Run itself needs one).
func resolveAndDeliverPacks(ctx context.Context, con *ui.Console, deps deliverDeps, a *apply.Applier, refs []config.PackRef, packs []*pack.Pack, renders []*pack.Rendered) (map[string][]string, error) {
	order, packDeps, err := pack.ResolveOrder(packs, refs, renders)
	if err != nil {
		return nil, err
	}
	for pos, i := range order {
		pref, rendered := refs[i], renders[i]
		// p6 DEP3: stamp each pack's resolved deps onto its Rendered before
		// delivery — deliverPack's engine calls (Deliver reads
		// rendered.DependsOn directly; DeliverGit takes it as a param) and
		// the wave gate below both read it from here, so this is the single
		// place the graph's packDeps enters the delivery tail.
		rendered.DependsOn = packDeps[packs[i].Name]
		// Each pack delivery is an enumerated open step (pack i+1/len(refs))
		// so renderers can show n-of-m; the Done message is byte-identical to
		// the previous con.Step line and plain never prints Dur — zero plain
		// drift.
		if err := func() error {
			pr := con.ProgressN("pack", "delivering "+pref.Ref, pos+1, len(refs))
			defer pr.Stop() // no-op after Done; resolves the step on any error return
			// p6 DEP3 wave gate: engines that cannot order deliveries
			// natively (OrdersDeliveries false — argocd) need `up` to block
			// here until every dependency's component is healthy, before
			// this pack's delivery is applied. Flux answers true and skips
			// straight through — its Kustomization dependsOn (stamped above)
			// orders reconciliation in-cluster instead.
			if !deps.eng.OrdersDeliveries() {
				if err := waitDepsHealthy(ctx, deps.eng, a, packs[i].Name, rendered.DependsOn, healthTimeout, healthPoll); err != nil {
					return err
				}
			}
			// The delivery tail branches on the ref's delivery mode:
			// deliverPackOCI is the OCI path; delivery: repo renders
			// into an engine-watched Gitea repo instead.
			if err := deliverPack(ctx, deps, pref, rendered); err != nil {
				return err
			}
			pr.Done("%s@%s delivered", rendered.Name, rendered.Version)
			return nil
		}(); err != nil {
			return nil, err
		}
	}
	return packDeps, nil
}

// waitDepsHealthy is the wave gate for engines that cannot order
// deliveries natively (OrdersDeliveries false — argocd; spec 2026-07-19
// DD5, ratified): before applying a dependent pack's delivery, poll
// Health until every dependency's component (cube-idp-<dep>) is Ready,
// bounded by timeout (the no-infinite-spinner rule). Flux never enters
// here — its Kustomization dependsOn orders reconciliation in-cluster.
func waitDepsHealthy(ctx context.Context, eng packEngine, a *apply.Applier, packName string, deps []string, timeout, poll time.Duration) error {
	if len(deps) == 0 {
		return nil
	}
	want := make(map[string]bool, len(deps))
	for _, d := range deps {
		want["cube-idp-"+d] = true
	}
	deadline := time.Now().Add(timeout)
	for {
		health, err := eng.Health(ctx, a)
		if err != nil {
			return err
		}
		ready := 0
		for _, h := range health {
			if want[h.Name] && h.Ready {
				ready++
			}
		}
		if ready == len(want) {
			return nil
		}
		if time.Now().After(deadline) {
			return diag.New(diag.CodeEngineDepWait,
				fmt.Sprintf("pack %s waits on %s — dependency not healthy within %s", packName, strings.Join(deps, ", "), timeout),
				"re-run `cube-idp up` (idempotent), or check the dependency with `cube-idp status`")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(poll):
		}
	}
}

// ensureSpoke creates/connects one spoke, bootstraps cube-idp RBAC on it,
// and applies the engine-native registration secret on the HUB (recorded
// in inventory so `spoke remove` + `up` prunes it, and `down` cascades).
func ensureSpoke(ctx context.Context, cube *config.Cube, sp config.SpokeSpec, hub *apply.Applier, con *ui.Console) error {
	sc := spokeClusterSpec(cube, sp)
	prov, err := cluster.New(sc, config.GatewaySpec{}) // zero gw: no host ports, no certs.d (S3 kindp guards)
	if err != nil {
		return diag.Wrap(err, diag.CodeSpokeEnsureFailed, fmt.Sprintf("spoke %q: unusable provider", sp.Name), "spokes support provider kind or existing")
	}
	sctx, cancel := context.WithTimeout(ctx, clusterTimeout)
	defer cancel()
	conn, err := prov.Ensure(sctx, spokeClusterName(cube, sp), sc)
	if err != nil {
		return diag.Wrap(err, diag.CodeSpokeEnsureFailed, fmt.Sprintf("spoke %q: cluster ensure failed", sp.Name), "`cube-idp doctor` preflights the runtime; for provider existing check the context name")
	}
	cred, err := spoke.Bootstrap(ctx, conn, cube.Spec.Engine.Type, applyTimeout)
	if err != nil {
		return err
	}
	server, err := spokeServerURL(ctx, prov, spokeClusterName(cube, sp), sp, conn)
	if err != nil {
		return err
	}
	secrets, err := spoke.HubSecrets(cube.Spec.Engine.Type, sp.Name, server, cred)
	if err != nil {
		return err
	}
	if err := hub.Apply(ctx, secrets, true, applyTimeout); err != nil {
		return diag.Wrap(err, diag.CodeSpokeRegisterFailed, fmt.Sprintf("spoke %q: hub registration apply failed", sp.Name), "is the hub engine namespace present? re-run `cube-idp up`")
	}
	if err := hub.RecordInventory(ctx, secrets); err != nil {
		return err
	}
	con.Log("spoke", "%s: server %s, sa cube-idp-%s", sp.Name, server, cube.Spec.Engine.Type)
	return nil
}

// spokeClusterSpec returns the effective ClusterSpec ensureSpoke hands the
// provider: kind spokes with no explicit kubernetesVersion inherit the
// hub's pin (or the documented "v1.33.1" default when the hub is provider
// existing and carries none, load.go's rule) — a bare kind spoke must
// never render the invalid node image "kindest/node:". Existing spokes
// pass through untouched: kubernetesVersion is a node-creation field.
func spokeClusterSpec(cube *config.Cube, sp config.SpokeSpec) config.ClusterSpec {
	sc := sp.Cluster
	if sc.Provider == "kind" && sc.KubernetesVersion == "" {
		sc.KubernetesVersion = cube.Spec.Cluster.KubernetesVersion
		if sc.KubernetesVersion == "" {
			sc.KubernetesVersion = "v1.33.1"
		}
	}
	return sc
}

// spokeClusterName: kind spokes get <cube>-spoke-<name> (this name is
// user-visible in `spoke remove` messages); existing
// spokes are whatever the context points at — Ensure ignores the name.
func spokeClusterName(cube *config.Cube, sp config.SpokeSpec) string {
	if sp.Cluster.Provider == "existing" {
		return sp.Name
	}
	return cube.Metadata.Name + "-spoke-" + sp.Name
}

// spokeServerURL picks the hub-reachable API endpoint: kind → internal
// kubeconfig's server (shared docker network); existing → the connection's
// own server URL (reachability is the operator's contract, doctor probes it).
func spokeServerURL(ctx context.Context, prov cluster.Provider, clusterName string, sp config.SpokeSpec, conn *kube.Conn) (string, error) {
	if ik, ok := prov.(cluster.InternalKubeconfiger); ok && sp.Cluster.Provider == "kind" {
		kc, err := ik.InternalKubeconfig(ctx, clusterName)
		if err != nil {
			return "", err
		}
		cfg, err := clientcmd.RESTConfigFromKubeConfig(kc)
		if err != nil {
			return "", diag.Wrap(err, diag.CodeSpokeEnsureFailed, "internal kubeconfig invalid", "recreate the spoke: cube-idp spoke remove --delete-cluster && cube-idp up")
		}
		return cfg.Host, nil
	}
	return conn.REST.Host, nil
}

// stepFetchSource emits the per-pack resolved-fetch-source step line —
// "fetching <source>" where source is exactly what pack.Fetch is about to
// read: the oci://... (or local/git) ref online, or the bundle-local staging
// dir (under a cube-idp-bundle-* temp dir) after resolveBundleRefs in
// --bundle mode. Added so offline honesty is
// falsifiable from output alone: the e2e bundle test asserts every fetch
// source points into the bundle and none is an oci:// ref — assertions that
// would FAIL on an online run, because this line demonstrably prints the
// network ref there (pinned by TestStepFetchSourcePlainOutput).
func stepFetchSource(con *ui.Console, ref string) {
	con.Step("pack", "fetching %s", ref)
}

// mergeImages returns the sorted, deduplicated union of rendered (images
// found by walking a pack's rendered manifests) and declared (pack.cue's
// optional images: list) — the Entry.Images the lock records.
// Operator-style packs (e.g. envoy-gateway) provision images that never
// appear in their own rendered objects, so `declared` closes that air-gap
// blind spot for `cube-idp vendor`, which builds the air-gap bundle purely
// from what cube.lock records.
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
// data-plane Service, the CoreDNS rewrite target for *.<gw.Host>
// (see docs/adr/0037-gateway-api-routing-surface.md). When the RESOLVED
// gateway pack (gwPack) declares a
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

// verifyGatewayPackRef guards an operator-found trap: gateway.ref silently wins over
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
	// The third long, previously-silent wait — health polling
	// can run for minutes while packs converge. pr spans the whole poll
	// loop; every error/timeout return below is unchanged from before
	// (nothing printed in plain mode), so pr.Stop() keeps that contract.
	// Each poll additionally emits a change-filtered HealthTick
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
				"re-run `cube-idp up` (idempotent); inspect the listed components with kubectl — deep dependsOn chains serialize startup, so deps reconcile before dependents")
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

// ---- per-pack delivery (delivery: oci|repo) ------------------------------

// Facts about the shipped gitea pack, mirrored from
// cmd/repo.go (verified against the shipped packs/gitea; tests/e2e keeps its own copy the same
// way): admin Secret gitea-admin-cube-idp in namespace gitea (keys
// username/password), chart-standard pod label, Service gitea-http:3000.
const (
	giteaNamespace       = "gitea"
	giteaAdminSecretName = "gitea-admin-cube-idp"
	giteaPodSelector     = "app.kubernetes.io/name=gitea"
	giteaPodPort         = 3000
	giteaInClusterHost   = "gitea-http.gitea.svc.cluster.local:3000"
	// giteaReadyPoll paces the repo-delivery readiness gate (giteaSession):
	// engine delivery is asynchronous, so gitea being DELIVERED does not
	// mean its API answers yet.
	giteaReadyPoll = 3 * time.Second
)

// packEngine is the narrow engine surface the delivery tail uses —
// engine.Engine satisfies it; the branch unit tests fake it. DeliverSelf
// rides the same seam: the cube-engine artifact push reuses deliverDeps'
// pushOCI/applier collaborators, so the selfManage unit tests share the
// per-pack delivery fakes.
type packEngine interface {
	Deliver(ctx context.Context, r *pack.Rendered, src engine.ArtifactRef) ([]*unstructured.Unstructured, error)
	DeliverGit(ctx context.Context, name string, src engine.GitSource, dependsOn []string) ([]*unstructured.Unstructured, error)
	DeliverSelf(ctx context.Context, src engine.ArtifactRef) ([]*unstructured.Unstructured, error)
	// OrdersDeliveries and Health back the p6 DEP3 wave gate
	// (waitDepsHealthy): an engine that answers false needs `up` to poll
	// Health itself before delivering a dependent pack.
	OrdersDeliveries() bool
	Health(ctx context.Context, a *apply.Applier) ([]engine.ComponentHealth, error)
}

// packApplier is the narrow Applier surface the delivery tail uses —
// *apply.Applier satisfies it; the branch unit tests fake it.
type packApplier interface {
	Apply(ctx context.Context, objs []*unstructured.Unstructured, wait bool, timeout time.Duration) error
	RecordInventory(ctx context.Context, objs []*unstructured.Unstructured) error
}

// giteaPacks is the narrow Gitea surface repo delivery uses —
// *gitea.Client satisfies it; the branch unit tests fake it.
type giteaPacks interface {
	EnsureRepo(ctx context.Context, name string) (*gitea.Repo, error)
	SyncDir(ctx context.Context, owner, repo, branch, dir, message string, files map[string][]byte) (bool, error)
}

// deliverDeps bundles the per-pack delivery collaborators so the
// oci-vs-repo branch is unit-testable with fakes: production wires the
// real engine, Applier, oci.PushRendered, and a lazy gitea session.
type deliverDeps struct {
	eng        packEngine
	applier    packApplier
	tunnelAddr string
	pushOCI    func(ctx context.Context, r *pack.Rendered, registryAddr string) (engine.ArtifactRef, error)
	// gitea yields the shared Gitea session, established lazily at the
	// first delivery: repo pack (the session builder owns the readiness
	// gate). OCI-delivered packs never invoke it.
	gitea func(ctx context.Context) (giteaPacks, error)
}

// deliverPack hands one rendered pack to the engine by the ref's delivery
// mode: "" or "oci" pushes to zot and registers an OCI source (the
// original single-mode tail, moved verbatim into deliverPackOCI); "repo" renders into an
// engine-watched Gitea repo (see docs/adr/0006-per-pack-delivery-mode.md).
// CUE constrains Delivery to the
// two values, so there is no third arm.
func deliverPack(ctx context.Context, deps deliverDeps, ref config.PackRef, rendered *pack.Rendered) error {
	if ref.Delivery == "repo" {
		return deliverPackRepo(ctx, deps, rendered)
	}
	return deliverPackOCI(ctx, deps, rendered)
}

// deliverPackOCI is the default per-pack delivery tail: push the render to
// the in-cluster zot and apply + inventory the engine's OCI source objects.
func deliverPackOCI(ctx context.Context, deps deliverDeps, rendered *pack.Rendered) error {
	artifact, err := deps.pushOCI(ctx, rendered, deps.tunnelAddr)
	if err != nil {
		return err
	}
	deliverObjs, err := deps.eng.Deliver(ctx, rendered, artifact)
	if err != nil {
		return err
	}
	if err := deps.applier.Apply(ctx, deliverObjs, false, applyTimeout); err != nil {
		return err
	}
	return deps.applier.RecordInventory(ctx, deliverObjs)
}

// deliverPackRepo is delivery: repo (docs/adr/0006-per-pack-delivery-mode.md): ensure the Gitea
// repo cube-pack-<name>, sync the RenderWith output (values + extras
// applied — cube.yaml is the source of truth, the repo the editable
// working copy) into its manifests/, and register an engine git source
// over the in-cluster clone URL instead of an OCI one. The lazy deps.gitea
// session carries the readiness gate, so by the time this runs the gitea
// API is answering.
func deliverPackRepo(ctx context.Context, deps deliverDeps, rendered *pack.Rendered) error {
	g, err := deps.gitea(ctx)
	if err != nil {
		return err
	}
	repo, err := g.EnsureRepo(ctx, "cube-pack-"+rendered.Name)
	if err != nil {
		return err
	}
	files, err := renderedFiles(rendered)
	if err != nil {
		return err
	}
	msg := fmt.Sprintf("cube-idp up: render %s@%s", rendered.Name, rendered.Version)
	if _, err := g.SyncDir(ctx, repo.Owner, repo.Name, repo.DefaultBranch, "manifests", msg, files); err != nil {
		return err
	}
	// The ENGINE clones over the in-cluster Service URL, never the
	// port-forward tunnel (cmd/repo.go's deployRepo, recorded 0.10
	// decision); failures past this point mirror its CUBE-7303 — the repo
	// itself already exists and re-running `up` is the retry.
	wrap := func(err error) error {
		return diag.Wrap(err, diag.CodeRepoDeployFail,
			fmt.Sprintf("pack %s: repo delivered but engine git source registration failed", rendered.Name),
			"re-run `cube-idp up` — it is idempotent and resumes where it left off")
	}
	src := engine.GitSource{
		URL:    fmt.Sprintf("http://%s/%s/%s.git", giteaInClusterHost, repo.Owner, repo.Name),
		Branch: repo.DefaultBranch,
		Path:   "./",
	}
	deliverObjs, err := deps.eng.DeliverGit(ctx, rendered.Name, src, rendered.DependsOn)
	if err != nil {
		return wrap(err)
	}
	if err := deps.applier.Apply(ctx, deliverObjs, false, applyTimeout); err != nil {
		return wrap(err)
	}
	if err := deps.applier.RecordInventory(ctx, deliverObjs); err != nil {
		return wrap(err)
	}
	return nil
}

// renderedFiles lays rendered.Objects out as manifests/NN-<kind>-<name>.yaml
// (order-indexed: the render's object order is the pack author's apply
// order, and stable names give stable git diffs across re-renders).
func renderedFiles(r *pack.Rendered) (map[string][]byte, error) {
	files := make(map[string][]byte, len(r.Objects))
	for i, o := range r.Objects {
		y, err := sigyaml.Marshal(o.Object)
		if err != nil {
			return nil, diag.Wrap(err, diag.CodeRepoDeployFail,
				fmt.Sprintf("pack %s: cannot marshal rendered object %d for repo delivery", r.Name, i),
				"this is a cube-idp bug — please report it")
		}
		name := fmt.Sprintf("manifests/%02d-%s-%s.yaml", i, strings.ToLower(o.GetKind()), o.GetName())
		files[name] = y
	}
	return files, nil
}

// orderPackRefs prepends the gateway pack ref. Ordering beyond that —
// including the gitea-before-repo-packs guarantee — moved to
// pack.ResolveOrder (p6 DEP2): the implicit repo→gitea edge plus the
// declared-order tie-break keep the guarantee; the giteaSession bounded
// gate still backstops the wait either way.
func orderPackRefs(gatewayRef string, packs []config.PackRef) []config.PackRef {
	return append([]config.PackRef{{Ref: gatewayRef}}, packs...)
}

// giteaSession is the repo-delivery readiness gate: engine
// reconciliation is asynchronous — the gitea pack being delivered does not
// mean its API is up — so the session is acquired by bounded polling of
// attempt (secret read -> port-forward -> API ping in production) until it
// succeeds or timeout elapses (typed CUBE-7301; the applyTimeout cap keeps
// the no-infinite-spinner rule). The hoisted gitea ordering makes this
// wait short; the gate makes the flow correct.
func giteaSession(ctx context.Context, timeout, poll time.Duration, attempt func(context.Context) (giteaPacks, func(), error)) (giteaPacks, func(), error) {
	deadline := time.Now().Add(timeout)
	for {
		g, stop, err := attempt(ctx)
		if err == nil {
			return g, stop, nil
		}
		if time.Now().After(deadline) {
			return nil, nil, diag.Wrap(err, diag.CodeRepoGiteaUnavailable,
				fmt.Sprintf("gitea is not ready within %s — repo-delivered packs need its API serving", timeout),
				"check `kubectl -n gitea get pods`, then re-run `cube-idp up` (idempotent) — or switch the pack to delivery: oci")
		}
		select {
		case <-ctx.Done():
			return nil, nil, diag.Wrap(ctx.Err(), diag.CodeRepoGiteaUnavailable,
				"gitea readiness wait aborted before completion",
				"re-run `cube-idp up` — it is idempotent and resumes where it left off")
		case <-time.After(poll):
		}
	}
}

// ---- engine self-management (engine.selfManage) --------------------------

// enginePreflightTimeout bounds the single eng.Health call of the
// self-management
// preflight — one LIST against the cluster, generously capped so a slow
// API server reads as "not healthy, SSA" rather than hanging `up`.
const enginePreflightTimeout = 10 * time.Second

// engineSelfTag is the fixed tag of the cube-engine artifact. Every `up`
// re-pushes the same tag (the digest moves, the tag never does), so the
// engine-native self-source watches one stable ref and each push is picked
// up as a new revision of it — no per-run tag garbage in zot.
const engineSelfTag = "latest"

// installNeedsSSA decides whether `up` server-side-applies the rendered
// engine install (see docs/adr/0020-engine-self-management-single-owner.md):
// selfManage off → always, the original behavior
// (Health is not even consulted); selfManage on → only on first install
// (rule 1) or when the engine is unhealthy at start (rule 3, self-brick
// recovery). A healthy self-managed engine owns itself and is never SSA'd
// (rule 2, single owner).
func installNeedsSSA(ctx context.Context, eng engine.Engine, a *apply.Applier, selfManage bool) bool {
	if !selfManage {
		return true
	}
	return !engineHealthyAtStart(ctx, eng, a)
}

// engineHealthyAtStart is the self-management preflight: one bounded eng.Health call
// (the same call waitHealthy polls). Healthy means components exist and
// every one is Ready. Tolerant of not-installed-yet by construction: on a
// fresh cluster the engine CRDs are absent, Health reports zero components
// with no error, and zero components is not-healthy — exactly rule 1's
// "first install". Any Health error also reads as not-healthy: when in
// doubt, SSA — re-applying the rendered install is idempotent.
func engineHealthyAtStart(ctx context.Context, eng engine.Engine, a *apply.Applier) bool {
	hctx, cancel := context.WithTimeout(ctx, enginePreflightTimeout)
	defer cancel()
	health, err := eng.Health(hctx, a)
	return err == nil && allReady(health)
}

// deliverEngineSelf is the single-owner tail (a healthy self-managed engine
// owns itself and is never SSA'd), run after the health gate when
// selfManage is on: push the ALREADY-rendered (tuned) engine install as the
// cube-engine artifact to zot over the registry tunnel, then apply +
// inventory the engine-native self-source objects. Rendering always
// happened in `up` before this — the artifact is finished YAML; the engine
// never sees tuning as a concept. The fresh reconcile-now annotation inside
// DeliverSelf's source object makes each apply double as the poke. Every
// failure arm is CUBE-3010 with re-run as the retry.
func deliverEngineSelf(ctx context.Context, deps deliverDeps, installObjs []*unstructured.Unstructured) error {
	wrap := func(err error, what string) error {
		return diag.Wrap(err, diag.CodeEngineSelfManage,
			"engine self-management: "+what,
			"re-run `cube-idp up` — it is idempotent and resumes where it left off")
	}
	selfRendered := &pack.Rendered{Name: engine.SelfArtifactName, Version: engineSelfTag, Objects: installObjs}
	artifact, err := deps.pushOCI(ctx, selfRendered, deps.tunnelAddr)
	if err != nil {
		return wrap(err, "cannot push the cube-engine artifact to zot")
	}
	selfObjs, err := deps.eng.DeliverSelf(ctx, artifact)
	if err != nil {
		return wrap(err, "cannot build the engine self-source")
	}
	if err := deps.applier.Apply(ctx, selfObjs, true, applyTimeout); err != nil {
		return wrap(err, "self-source apply failed")
	}
	if err := deps.applier.RecordInventory(ctx, selfObjs); err != nil {
		return wrap(err, "self-source inventory write failed")
	}
	return nil
}

// giteaConnectOnce is one production acquisition attempt for giteaSession:
// read the gitea pack's admin Secret, port-forward to the gitea pod, and
// prove the API answers. Errors are plain (the gate retries them); only
// the gate's terminal timeout is typed.
func giteaConnectOnce(ctx context.Context, restCfg *rest.Config, cl client.Client) (giteaPacks, func(), error) {
	var sec corev1.Secret
	key := client.ObjectKey{Namespace: giteaNamespace, Name: giteaAdminSecretName}
	if err := cl.Get(ctx, key, &sec); err != nil {
		return nil, nil, fmt.Errorf("gitea admin secret not readable yet: %w", err)
	}
	addr, stop, err := kube.PortForward(ctx, restCfg, giteaNamespace, giteaPodSelector, giteaPodPort)
	if err != nil {
		return nil, nil, fmt.Errorf("gitea pod not port-forwardable yet: %w", err)
	}
	gc := &gitea.Client{BaseURL: "http://" + addr, Username: string(sec.Data["username"]), Password: string(sec.Data["password"])}
	if err := gc.Ping(ctx); err != nil {
		stop()
		return nil, nil, fmt.Errorf("gitea API not answering yet: %w", err)
	}
	return gc, stop, nil
}
