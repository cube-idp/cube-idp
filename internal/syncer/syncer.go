// Package syncer implements D7's one-shot half (spec §4, Task 10):
// `cube-idp sync <dir>` renders dir as a pack, pushes it to the cube's zot
// registry through a port-forward tunnel, delivers it through the engine,
// applies + records the delivery objects in the inventory, and Pokes the
// engine to reconcile now instead of on its poll interval. Task 11 adds the
// fsnotify --watch loop around SyncOnce.
package syncer

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"k8s.io/client-go/rest"

	"github.com/rafpe/cube-idp/internal/apply"
	"github.com/rafpe/cube-idp/internal/diag"
	"github.com/rafpe/cube-idp/internal/engine"
	"github.com/rafpe/cube-idp/internal/kube"
	"github.com/rafpe/cube-idp/internal/oci"
	"github.com/rafpe/cube-idp/internal/pack"
	"github.com/rafpe/cube-idp/internal/ui"
)

// syncApplyTimeout bounds the wait=false server-side apply of the delivery
// objects. wait=false because sync's whole point is a fast local loop —
// waiting for the engine's own reconcile (which Poke expedites) belongs to
// `cube-idp status`, not sync.
const syncApplyTimeout = 2 * time.Minute

// Result is what SyncOnce reports back to its caller (cmd/sync.go). The
// "delivered — engine reconciling" line (below) is the only user-facing
// confirmation of a completed sync; cmd/sync.go does not print a second
// summary from Result to avoid duplicating it. Digest feeds Task 11's
// change-skip logic (compare the digest of the last sync to skip a no-op
// push).
type Result struct{ Pack, Version, Digest string }

// Stepper is the emitter seam SyncOnce prints its progress lines through —
// both *ui.Console and *ui.Printer satisfy it as-is (G6: identical Step
// signatures), so the one-shot path (wrapped in ui.RunPipelineStatic,
// cmd/sync.go) and the watch path (still a raw ui.Printer, out of scope for
// Task R3 — spec §5.3) share SyncOnce without an adapter.
type Stepper interface {
	Step(stage, format string, args ...any)
}

// Deps is SyncOnce's dependency bag, assembled by cmd/sync.go exactly like
// status/down connect (config -> provider Ensure -> Applier -> engine
// factory) and injected here for testability.
type Deps struct {
	Applier *apply.Applier
	Engine  engine.Engine
	REST    *rest.Config
	Out     io.Writer
	// Steps is the progress emitter SyncOnce calls Step through. nil
	// defaults to ui.NewFor(Out) — the pre-R3 behavior, and what Watch's
	// un-migrated path (internal/syncer/watch.go) still relies on by never
	// setting this field.
	Steps Stepper
	// PushAddr optionally overrides the zot port-forward tunnel — tests
	// inject a local registry address instead of port-forwarding to a real
	// cluster. Production leaves it empty.
	PushAddr string
	// syncFn is Watch's injectable seam (Task 11): when set, Watch calls
	// this instead of building a SyncOnce closure — the watch loop's tests
	// exercise the debounce/error/cancellation machinery with a fake
	// syncFn instead of a real cluster. Production leaves it nil.
	syncFn func(context.Context) error
}

// SyncOnce is D7's one-shot iteration:
//
//	p, cleanup := loadOrSynthesize(dir); defer cleanup()  -> CUBE-7201
//	rendered := p.Render(nil)                             -> pack's own CUBE-4xxx codes pass through
//	addr := deps.PushAddr; if empty, port-forward to zot   -> CUBE-5002
//	ref := oci.PushRendered(ctx, rendered, addr)           -> CUBE-5003 passes through
//	objs := deps.Engine.Deliver(ctx, rendered, ref)
//	deps.Applier.Apply(ctx, objs, false, syncApplyTimeout) // idempotent SSA — safe every iteration
//	deps.Applier.RecordInventory(ctx, objs)                // MERGES with pre-existing entries (Owner Decisions #14)
//	deps.Engine.Poke(ctx, deps.Applier, rendered.Name)
//
// Every step's error is already typed by the layer that produced it —
// SyncOnce adds no codes of its own beyond CUBE-7201 (from loadOrSynthesize).
func SyncOnce(ctx context.Context, deps Deps, dir string) (Result, error) {
	p, cleanup, err := loadOrSynthesize(dir)
	if err != nil {
		return Result{}, err
	}
	defer cleanup()

	rendered, err := p.Render(nil)
	if err != nil {
		return Result{}, err
	}

	steps := deps.Steps
	if steps == nil {
		steps = ui.NewFor(deps.Out)
	}
	steps.Step("sync", "%s@%s rendered (%d object(s))", rendered.Name, rendered.Version, len(rendered.Objects))

	addr := deps.PushAddr
	if addr == "" {
		var stop func()
		addr, stop, err = kube.PortForward(ctx, deps.REST, apply.SystemNamespace, "app=zot", 5000)
		if err != nil {
			return Result{}, diag.Wrap(err, diag.CodePortForwardFail, "port-forward to zot failed",
				"re-run `cube-idp up`; check `kubectl -n cube-idp-system get pods`")
		}
		defer stop()
	}

	ref, err := oci.PushRendered(ctx, rendered, addr)
	if err != nil {
		return Result{}, err
	}
	steps.Step("sync", "pushed packs/%s:%s", rendered.Name, rendered.Version)

	objs, err := deps.Engine.Deliver(ctx, rendered, ref)
	if err != nil {
		return Result{}, err
	}
	if err := deps.Applier.Apply(ctx, objs, false, syncApplyTimeout); err != nil {
		return Result{}, err
	}
	// RecordInventory MERGES (object.ObjMetadataSet.Union with the loaded
	// existing set — internal/apply/inventory.go) so this can never orphan
	// entries `up` (or a previous sync) already recorded.
	if err := deps.Applier.RecordInventory(ctx, objs); err != nil {
		return Result{}, err
	}
	if err := deps.Engine.Poke(ctx, deps.Applier, rendered.Name); err != nil {
		return Result{}, err
	}
	steps.Step("sync", "%s@%s delivered — engine reconciling", rendered.Name, rendered.Version)

	return Result{Pack: rendered.Name, Version: rendered.Version, Digest: ref.Digest}, nil
}

// loadOrSynthesize loads dir as a pack. A dir with a pack.cue is fetched
// normally (pack.Fetch's local-directory path — CUE metadata, #Values
// schema, chart.yaml/kustomization.yaml all apply as usual); its cleanup is
// a no-op — dir is the caller's own directory, not staging. A dir with no
// pack.cue is a bare manifest directory (D7): every *.yaml/*.yml file
// directly under dir is staged into a synthesized pack in a fresh
// os.MkdirTemp directory whose identity is name = filepath.Base(dir),
// version = "0.0.0-dev"; its cleanup removes that staging directory (Phase 4
// R8 — previously leaked one temp dir per bare-dir sync). A dir with neither
// a pack.cue nor any renderable manifest is CUBE-7201.
func loadOrSynthesize(dir string) (*pack.Pack, func(), error) {
	noop := func() {}
	if _, err := os.Stat(filepath.Join(dir, "pack.cue")); err == nil {
		p, err := pack.Fetch(context.Background(), dir, "")
		return p, noop, err
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, noop, diag.Wrap(err, diag.CodeSyncNoManifests, fmt.Sprintf("cannot read %s", dir),
			"check the directory path and permissions")
	}

	tmp, err := os.MkdirTemp("", "cube-idp-sync-*")
	if err != nil {
		return nil, noop, diag.Wrap(err, diag.CodeSyncNoManifests, "cannot stage synthesized pack",
			"check available disk space and permissions on the OS temp directory")
	}
	cleanup := func() { os.RemoveAll(tmp) }
	manifestsDir := filepath.Join(tmp, "manifests")
	if err := os.MkdirAll(manifestsDir, 0o755); err != nil {
		return nil, cleanup, diag.Wrap(err, diag.CodeSyncNoManifests, "cannot stage synthesized pack",
			"check available disk space and permissions on the OS temp directory")
	}

	staged := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := filepath.Ext(e.Name())
		if ext != ".yaml" && ext != ".yml" {
			continue
		}
		src := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(src)
		if err != nil {
			return nil, cleanup, diag.Wrap(err, diag.CodeSyncNoManifests, fmt.Sprintf("cannot read %s", src), "check file permissions")
		}
		if err := os.WriteFile(filepath.Join(manifestsDir, e.Name()), data, 0o644); err != nil {
			return nil, cleanup, diag.Wrap(err, diag.CodeSyncNoManifests, "cannot stage synthesized pack",
				"check available disk space and permissions on the OS temp directory")
		}
		staged++
	}
	if staged == 0 {
		return nil, cleanup, diag.New(diag.CodeSyncNoManifests,
			fmt.Sprintf("%s has no pack.cue and no *.yaml/*.yml manifests", dir),
			"add a pack.cue (for a full pack) or at least one manifest YAML file directly under the directory")
	}

	name, version := filepath.Base(filepath.Clean(dir)), "0.0.0-dev"
	// Pack.Render reads pack.cue itself (validateValues), even for a
	// directly-constructed *Pack — so the synthesized identity needs a
	// matching, schema-less pack.cue staged alongside manifests/, not just
	// the Pack struct's Name/Version fields.
	packCUE := fmt.Sprintf("name: %q\nversion: %q\n", name, version)
	if err := os.WriteFile(filepath.Join(tmp, "pack.cue"), []byte(packCUE), 0o644); err != nil {
		return nil, cleanup, diag.Wrap(err, diag.CodeSyncNoManifests, "cannot stage synthesized pack",
			"check available disk space and permissions on the OS temp directory")
	}

	return &pack.Pack{Name: name, Version: version, Dir: tmp}, cleanup, nil
}
