// Package up orchestrates `cube-idp up` (spec §4.3). It sequences the
// already-tested units and owns user-facing progress output. It has no
// logic of its own beyond ordering and timeouts — keep it that way.
package up

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rafpe/cube-idp/internal/apply"
	"github.com/rafpe/cube-idp/internal/cluster"
	"github.com/rafpe/cube-idp/internal/config"
	"github.com/rafpe/cube-idp/internal/diag"
	"github.com/rafpe/cube-idp/internal/engine"
	enginefactory "github.com/rafpe/cube-idp/internal/engine/factory"
	"github.com/rafpe/cube-idp/internal/oci"
	"github.com/rafpe/cube-idp/internal/pack"
	"github.com/rafpe/cube-idp/internal/registry"
)

const (
	clusterTimeout = 3 * time.Minute
	applyTimeout   = 2 * time.Minute
	healthTimeout  = 5 * time.Minute
	healthPoll     = 5 * time.Second
)

// Run drives the full up sequence for the cube.yaml at cfgPath, writing
// progress to out: load config -> ensure cluster -> install registry ->
// install engine -> port-forward the registry -> fetch/render/push/deliver
// every pack (gateway first) -> wait for engine-reported health -> print a
// success summary.
func Run(ctx context.Context, cfgPath string, out io.Writer) error {
	cube, err := config.Load(cfgPath)
	if err != nil {
		return err
	}
	step(out, "config", "cube %q loaded and validated", cube.Metadata.Name)

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

	tunnelAddr, stop, err := registry.PortForward(ctx, conn.REST)
	if err != nil {
		return err
	}
	defer stop()

	dir, err := cacheDir()
	if err != nil {
		return err
	}

	// Gateway pack goes first — everything else depends on ingress existing.
	refs := append([]config.PackRef{{Ref: gatewayPackRef(cube.Spec.Gateway)}}, cube.Spec.Packs...)
	for _, pr := range refs {
		p, err := pack.Fetch(ctx, pr.Ref, dir)
		if err != nil {
			return err
		}
		rendered, err := p.Render(pr.Values)
		if err != nil {
			return err
		}
		artifact, err := oci.PushRendered(ctx, rendered, tunnelAddr)
		if err != nil {
			return err
		}
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

	if err := waitHealthy(ctx, eng, a, out, healthTimeout); err != nil {
		return err
	}

	// Phase 1 is HTTP end-to-end; TLS arrives with `cube-idp trust` (Phase 2).
	fmt.Fprintf(out, "\n✔ cube %q is up — http://%s:%d\n  credentials: cube-idp get secrets\n",
		cube.Metadata.Name, cube.Spec.Gateway.Host, cube.Spec.Gateway.Port)
	return nil
}

// gatewayPackRef resolves the pack source `up` fetches for the gateway
// pack: an explicit gw.Ref always wins; otherwise it falls back to
// "packs/<Pack>", a path that only resolves when cube-idp runs from a
// checkout of its own repo. `cube-idp init --local <repo>` sets Ref to an
// absolute path so `up` works from any working directory.
func gatewayPackRef(gw config.GatewaySpec) string {
	if gw.Ref != "" {
		return gw.Ref
	}
	return "packs/" + gw.Pack
}

// cacheDir returns (creating if needed) the local pack cache directory,
// $HOME/.cache/cube-idp/packs.
func cacheDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", diag.Wrap(err, "CUBE-4013", "cannot determine home directory for the pack cache",
			"set $HOME, or check your environment")
	}
	dir := filepath.Join(home, ".cache", "cube-idp", "packs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", diag.Wrap(err, "CUBE-4013", fmt.Sprintf("cannot create pack cache dir %s", dir),
			"check permissions on $HOME/.cache")
	}
	return dir, nil
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
			return diag.New("CUBE-3004",
				fmt.Sprintf("timed out after %s waiting for components to become healthy: %s",
					timeout, unreadySummary(health)),
				"re-run `cube-idp up` (idempotent); inspect the listed components with kubectl")
		}
		select {
		case <-ctx.Done():
			return diag.Wrap(ctx.Err(), "CUBE-3004", "health wait aborted before completion",
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

// step prints one line of user-facing progress.
func step(w io.Writer, stage, format string, args ...any) {
	fmt.Fprintf(w, "▸ [%s] %s\n", stage, fmt.Sprintf(format, args...))
}
