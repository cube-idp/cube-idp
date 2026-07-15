package syncer

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/rafpe/cube-idp/internal/ui"
)

// TestSyncOncePlainByteStable is Step 3.3's golden test: a recorded sync
// event slice — the exact three Step calls SyncOnce makes on a successful
// run (G7's pinned bytes) — driven through the real Stepper seam (deps.Steps
// = *ui.Console via ui.RunPipeline with ModePlain forced) must produce
// exactly today's three-line plain sequence. A full fake sync through a real
// Applier needs envtest (synconce_test.go's
// TestSyncOnceMergesInventoryWithPreexistingEntries already covers that
// path); this test isolates the Stepper/output-format seam on the recorded
// slice instead (14b precedent: renderer goldens run on recorded slices).
func TestSyncOncePlainByteStable(t *testing.T) {
	var buf bytes.Buffer
	err := ui.RunPipeline(context.Background(), "sync", &buf,
		func(_ context.Context, con *ui.Console) error {
			con.Start("sync", "dev")
			con.Step("sync", "%s@%s rendered (%d object(s))", "demo", "0.0.0-dev", 1)
			con.Step("sync", "pushed packs/%s:%s", "demo", "0.0.0-dev")
			con.Step("sync", "%s@%s delivered — engine reconciling", "demo", "0.0.0-dev")
			return nil
		})
	if err != nil {
		t.Fatal(err)
	}
	const want = "▸ [sync] demo@0.0.0-dev rendered (1 object(s))\n" +
		"▸ [sync] pushed packs/demo:0.0.0-dev\n" +
		"▸ [sync] demo@0.0.0-dev delivered — engine reconciling\n"
	if got := buf.String(); got != want {
		t.Fatalf("plain projection drifted:\ngot:  %q\nwant: %q", got, want)
	}
}

// TestSyncOnceJSONStreamEmitsExpectedEventTypes is the JSON-mode leg: the
// same recorded call shape through ModeJSON emits run_started, three
// step_done events, and a terminal run_done{ok:true}.
func TestSyncOnceJSONStreamEmitsExpectedEventTypes(t *testing.T) {
	prev := ui.CurrentMode()
	ui.SetMode(ui.ModeJSON)
	defer ui.SetMode(prev)

	var buf bytes.Buffer
	err := ui.RunPipelineStatic(context.Background(), "sync", &buf,
		func(_ context.Context, con *ui.Console) error {
			con.Start("sync", "dev")
			con.Step("sync", "%s@%s rendered (%d object(s))", "demo", "0.0.0-dev", 1)
			con.Step("sync", "pushed packs/%s:%s", "demo", "0.0.0-dev")
			con.Step("sync", "%s@%s delivered — engine reconciling", "demo", "0.0.0-dev")
			return nil
		})
	if err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	if !strings.Contains(got, `"type":"run_started","cmd":"sync","cube":"dev"`) {
		t.Fatalf("missing run_started: %s", got)
	}
	if n := strings.Count(got, `"type":"step_done"`); n != 3 {
		t.Fatalf("want 3 step_done events, got %d:\n%s", n, got)
	}
	if !strings.Contains(got, `"type":"run_done","ok":true`) {
		t.Fatalf("missing run_done{ok:true}: %s", got)
	}
}
