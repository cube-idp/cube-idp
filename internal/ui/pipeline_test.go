package ui

import (
	"bytes"
	"context"
	"errors"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/rafpe/cube-idp/internal/diag"
	"github.com/rafpe/cube-idp/internal/ui/event"
)

// TestRunPipelinePlainByteNeutrality drives the full production path —
// Console facade → RunPipeline → plain renderer — with the exact §4.3 call
// shapes up.Run uses, and pins the output to the pre-14b bytes plus only
// the ratified Access block. Together with the renderer goldens this is the
// automated `up | cat` byte-neutrality proof.
func TestRunPipelinePlainByteNeutrality(t *testing.T) {
	var out bytes.Buffer // never a TTY -> plain, no SetMode needed
	err := RunPipeline(context.Background(), "up", &out,
		func(_ context.Context, con *Console) error {
			con.Start("up", "dev")
			con.Step("config", "cube %q loaded and validated", "dev")
			pr := con.Progress("cluster", "creating kind cluster")
			pr.Done("%s cluster ready (context %s)", "kind", "kind-dev")
			con.Health([]event.ComponentState{{Name: "cube-idp-traefik", Ready: true, Message: "ok"}})
			con.Note("\n✔ cube %q is up — https://%s:%d\n  credentials: cube-idp get secrets", "dev", "cube.local", 8443)
			con.Access([]event.PackAccess{{Name: "gitea", URLs: []string{"https://gitea.cube.local:8443"}}},
				"credentials: cube-idp get secrets")
			return nil
		})
	if err != nil {
		t.Fatal(err)
	}
	const want = "▸ [config] cube \"dev\" loaded and validated\n" +
		"▸ [cluster] kind cluster ready (context kind-dev)\n" +
		"\n✔ cube \"dev\" is up — https://cube.local:8443\n  credentials: cube-idp get secrets\n" +
		"\nAccess\n" +
		"  gitea        https://gitea.cube.local:8443\n" +
		"  credentials: cube-idp get secrets\n"
	if got := out.String(); got != want {
		t.Fatalf("pipeline plain output drifted:\ngot:  %q\nwant: %q", got, want)
	}
}

// TestRunPipelineFailureOrderAndErrorPassthrough pins the §4.2 failure
// lifecycle: StepFailed for the still-open stage, RunDone{OK:false}, then
// Diagnosis TERMINAL — and the producer's error returned verbatim so
// cobra/main.go error handling is unchanged.
func TestRunPipelineFailureOrderAndErrorPassthrough(t *testing.T) {
	wantErr := diag.New(diag.Code("CUBE-9999"), "boom", "run it again")

	// The JSON stream is the event order made visible — parse its types in
	// order to assert the lifecycle sequence.
	prev := CurrentMode()
	SetMode(ModeJSON)
	defer SetMode(prev)
	var out bytes.Buffer
	err := RunPipeline(context.Background(), "up", &out,
		func(_ context.Context, con *Console) error {
			con.Start("up", "dev")
			con.Progress("cluster", "creating kind cluster") // left open: the error unwinds past it
			return wantErr
		})
	if !errors.Is(err, wantErr) {
		t.Fatalf("RunPipeline must return the producer's error verbatim, got %v", err)
	}

	var order []string
	for _, line := range strings.Split(strings.TrimRight(out.String(), "\n"), "\n") {
		for _, typ := range []string{"run_started", "step_started", "step_failed", "run_done", "diagnosis"} {
			if strings.Contains(line, `"type":"`+typ+`"`) {
				order = append(order, typ)
			}
		}
	}
	want := []string{"run_started", "step_started", "step_failed", "run_done", "diagnosis"}
	if strings.Join(order, ",") != strings.Join(want, ",") {
		t.Fatalf("failure lifecycle order drifted:\ngot:  %v\nwant: %v\nstream:\n%s", order, want, out.String())
	}
	if !strings.Contains(out.String(), `"code":"CUBE-9999"`) {
		t.Fatalf("diagnosis must carry the typed code:\n%s", out.String())
	}
	if !strings.HasSuffix(strings.TrimRight(out.String(), "\n"),
		`"raw":"CUBE-9999: boom"}`) {
		t.Fatalf("diagnosis must be the final line:\n%s", out.String())
	}
}

// TestRunPipelineNoGoroutineSurvives pins §4.2 contract (b): by the time
// RunPipeline returns, every goroutine it started has exited — success and
// failure paths both.
func TestRunPipelineNoGoroutineSurvives(t *testing.T) {
	before := runtime.NumGoroutine()
	for i := 0; i < 10; i++ {
		var out bytes.Buffer
		_ = RunPipeline(context.Background(), "up", &out,
			func(_ context.Context, con *Console) error {
				con.Step("config", "ok")
				return nil
			})
		_ = RunPipeline(context.Background(), "up", &out,
			func(_ context.Context, con *Console) error {
				con.Progress("cluster", "left open")
				return errors.New("fail")
			})
	}
	// Allow the runtime a moment to reap exited goroutines before comparing.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if runtime.NumGoroutine() <= before {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("goroutines leaked across RunPipeline: before=%d after=%d", before, runtime.NumGoroutine())
}

// TestRunPipelineContextCancelUnwinds proves Ctrl-C's path: cancelling the
// command context lets the producer unwind through its normal error path
// and the pipeline still terminates with the producer's error.
func TestRunPipelineContextCancelUnwinds(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var out bytes.Buffer
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	err := RunPipeline(ctx, "up", &out,
		func(runCtx context.Context, con *Console) error {
			pr := con.Progress("health", "waiting")
			<-runCtx.Done()
			pr.Stop()
			return runCtx.Err()
		})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled through the pipeline, got %v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("plain projection of an aborted progress must be zero bytes, got %q", out.String())
	}
}

// TestRunPipelineStaticNeverGoesLive: under ModeStyled with a non-TTY writer
// the projection is plain (byte-identical to render.Plain for the same
// events); under ModeJSON it is the JSON stream. (A true-TTY styled
// assertion is render/styled_test.go's job — pipeline_test can only prove
// the non-TTY and JSON legs plus that no live program ever starts.)
func TestRunPipelineStaticNeverGoesLive(t *testing.T) {
	defer SetMode(CurrentMode())
	SetMode(ModeStyled)
	var buf bytes.Buffer
	err := RunPipelineStatic(context.Background(), "pack", &buf,
		func(ctx context.Context, con *Console) error {
			con.Step("pack", "pushed oci://x/y:1@sha256:abc")
			return nil
		})
	if err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); got != "▸ [pack] pushed oci://x/y:1@sha256:abc\n" {
		t.Fatalf("plain projection: %q", got)
	}
}

// TestRunPipelineStaticJSONMode proves the JSON leg behaves exactly like
// RunPipeline: one event per line, terminal ordering intact.
func TestRunPipelineStaticJSONMode(t *testing.T) {
	defer SetMode(CurrentMode())
	SetMode(ModeJSON)
	var buf bytes.Buffer
	err := RunPipelineStatic(context.Background(), "pack", &buf,
		func(ctx context.Context, con *Console) error {
			con.Start("pack", "")
			con.Step("pack", "pushed oci://x/y:1@sha256:abc")
			return nil
		})
	if err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	if !strings.Contains(got, `"type":"run_started"`) || !strings.Contains(got, `"type":"step_done"`) ||
		!strings.Contains(got, `"type":"run_done"`) {
		t.Fatalf("JSON projection missing expected event types:\n%s", got)
	}
}

// TestConsoleHealthChangeFilter pins the HealthTick contract: first poll
// always emits; identical subsequent polls emit nothing; any change emits.
func TestConsoleHealthChangeFilter(t *testing.T) {
	var out bytes.Buffer
	prev := CurrentMode()
	SetMode(ModeJSON)
	defer SetMode(prev)
	err := RunPipeline(context.Background(), "up", &out,
		func(_ context.Context, con *Console) error {
			same := []event.ComponentState{{Name: "a", Ready: false, Message: "reconciling"}}
			con.Health(same)
			con.Health(same)
			con.Health(same)
			con.Health([]event.ComponentState{{Name: "a", Ready: true, Message: "ok"}})
			return nil
		})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(out.String(), `"type":"health_tick"`); got != 2 {
		t.Fatalf("change filter drifted: want 2 health_tick lines, got %d:\n%s", got, out.String())
	}
}
