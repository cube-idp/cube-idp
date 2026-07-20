package render

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"

	"github.com/cube-idp/cube-idp/internal/ui/event"
	"github.com/cube-idp/cube-idp/internal/ui/theme"
)

// sbLines feeds evs through one stateful scrollback and collects every
// emitted line — the exact sequence Live()'s forwarder would p.Println.
func sbLines(sb *scrollback, evs ...event.Event) []string {
	var out []string
	for _, ev := range evs {
		out = append(out, sb.lines(ev)...)
	}
	return out
}

func readGolden(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("golden %s: %v", name, err)
	}
	return string(b)
}

// TestScrollbackLineContentIdentical pins the §5.2 content-identical rule:
// styled scrollback lines carry the same stage tags and words as the plain
// projection — presentation (glyph, color, alignment, duration suffix)
// only.
func TestScrollbackLineContentIdentical(t *testing.T) {
	sb := newScrollback(theme.New(true))

	done := strings.Join(sb.lines(event.StepDone{Stage: "cluster", Msg: "kind cluster ready (context kind-dev)", Dur: 12 * time.Second}), "\n")
	for _, want := range []string{"✔", "[cluster]", "kind cluster ready (context kind-dev)", "(12s)"} {
		if !strings.Contains(done, want) {
			t.Fatalf("StepDone scrollback missing %q: %q", want, done)
		}
	}
	if instant := strings.Join(sb.lines(event.StepDone{Stage: "tls", Msg: "ready"}), "\n"); strings.Contains(instant, "(") {
		t.Fatalf("Dur==0 must not print a duration: %q", instant)
	}

	// A failed step is never a naked ✗ — an empty Msg falls
	// back to the word "failed".
	failed := strings.Join(sb.lines(event.StepFailed{Stage: "engine"}), "\n")
	for _, want := range []string{"✗", "[engine]", "failed"} {
		if !strings.Contains(failed, want) {
			t.Fatalf("StepFailed scrollback missing %q: %q", want, failed)
		}
	}

	// R2: the epilogue no longer travels as a Note — this sample is just a
	// neutral passthrough line (glyph-free, like all Note content now).
	const noteMsg = "\ncube \"dev\" is up — https://cube.local:8443\n  credentials: cube-idp get secrets"
	if got := sb.lines(event.Note{Msg: noteMsg}); len(got) != 1 || got[0] != noteMsg {
		t.Fatalf("Note must pass through verbatim:\ngot:  %q\nwant: %q", got, noteMsg)
	}

	// Epilogue block: renderer-supplied ✔ headline, key-value rows, next: hint.
	epi := strings.Join(sb.lines(event.Epilogue{Cube: "dev", GatewayURL: "https://cube.local:8443",
		Hint: "credentials: cube-idp get secrets"}), "\n")
	for _, want := range []string{"✔", `cube "dev" is up`, "gateway", "https://cube.local:8443",
		"next: cube-idp status · credentials: cube-idp get secrets"} {
		if !strings.Contains(epi, want) {
			t.Fatalf("Epilogue scrollback missing %q: %q", want, epi)
		}
	}
	if strings.Contains(epi, "context") || strings.Contains(epi, "registry") {
		t.Fatalf("empty Epilogue fields must not render rows: %q", epi)
	}

	if warn := strings.Join(sb.lines(event.Warn{Msg: "heads up"}), "\n"); !strings.Contains(warn, "⚠") || !strings.Contains(warn, "heads up") {
		t.Fatalf("Warn scrollback drifted: %q", warn)
	}

	access := strings.Join(sb.lines(event.Access{
		Packs: []event.PackAccess{{Name: "gitea", URLs: []string{"https://gitea.cube.local:8443"}}},
		Hint:  "credentials: cube-idp get secrets",
	}), "\n")
	for _, want := range []string{"Access", "gitea", "https://gitea.cube.local:8443", "credentials: cube-idp get secrets"} {
		if !strings.Contains(access, want) {
			t.Fatalf("Access scrollback missing %q: %q", want, access)
		}
	}
}

// TestScrollbackSilentEvents pins that region-only events never print to
// scrollback — the Diagnosis in particular renders AFTER program exit via
// main.go's ui.RenderError, never here (diagnosis-last, §5.2). RunDone left
// this set in T05: after a RunStarted it prints the run summary line
// without one (config.Load failed) it stays silent.
func TestScrollbackSilentEvents(t *testing.T) {
	sb := newScrollback(theme.New(true))
	silent := []event.Event{
		event.RunStarted{Cmd: "up", Cube: "dev"},
		event.StepStarted{Stage: "cluster", Msg: "creating"},
		event.StepLog{Stage: "cluster", Line: "buffered, never printed on its own"},
		event.HealthTick{Components: []event.ComponentState{{Name: "x"}}},
		event.Diagnosis{Raw: "boom"},
	}
	for _, ev := range silent {
		if got := sb.lines(ev); len(got) != 0 {
			t.Fatalf("%T must not reach scrollback, got %q", ev, got)
		}
	}
	if got := sb.lines(event.RunDone{OK: false, Dur: time.Second}); len(got) != 1 ||
		!strings.Contains(got[0], "✗") || !strings.Contains(got[0], "up failed after 1s") {
		t.Fatalf("RunDone after RunStarted must print the run summary, got %q", got)
	}
	if got := newScrollback(theme.New(true)).lines(event.RunDone{OK: false, Dur: time.Second}); len(got) != 0 {
		t.Fatalf("RunDone without RunStarted must stay silent, got %q", got)
	}
}

func apply(t *testing.T, m liveModel, evs ...event.Event) liveModel {
	t.Helper()
	for _, ev := range evs {
		next, _ := m.Update(evMsg{ev})
		m = next.(liveModel)
	}
	return m
}

// TestLiveRegionLifecycle drives the model event-by-event and asserts the
// managed region's state: spinner lines appear per open step and vanish on
// resolution; the health table persists from its first HealthTick until
// RunDone (closing the health stage no longer drops it);
// RunDone collapses the region to zero lines.
func TestLiveRegionLifecycle(t *testing.T) {
	m := newLiveModel(func() {})
	m.now = func() time.Time { return time.Unix(0, 0) } // frozen: elapsed is deterministic

	m = apply(t, m, event.RunStarted{Cmd: "up", Cube: "dev"})
	if v := m.View().Content; !strings.Contains(v, `cube "dev"`) {
		t.Fatalf("header missing after RunStarted: %q", v)
	}

	m = apply(t, m, event.StepStarted{Stage: "cluster", Msg: "creating kind cluster"})
	if v := m.View().Content; !strings.Contains(v, "[cluster]") || !strings.Contains(v, "creating kind cluster") {
		t.Fatalf("in-flight spinner line missing: %q", v)
	}

	m = apply(t, m, event.StepDone{Stage: "cluster", Msg: "ready"})
	if v := m.View().Content; strings.Contains(v, "[cluster]") {
		t.Fatalf("resolved step must leave the region: %q", v)
	}

	m = apply(t, m,
		event.StepStarted{Stage: "health", Msg: "waiting for components to become ready"},
		event.HealthTick{Components: []event.ComponentState{
			{Name: "cube-idp-traefik", Ready: false, Message: "reconciling"},
			{Name: "cube-idp-gitea", Ready: true, Message: "Applied revision"},
		}})
	v := m.View().Content
	for _, want := range []string{"cube-idp-traefik", "reconciling", "cube-idp-gitea", "✔", "✗"} {
		if !strings.Contains(v, want) {
			t.Fatalf("health table missing %q while health stage open: %q", want, v)
		}
	}

	m = apply(t, m, event.StepDone{Stage: "health", Msg: "2 component(s) ready"})
	if v := m.View().Content; !strings.Contains(v, "cube-idp-traefik") {
		t.Fatalf("health snapshot must persist after its stage closes: %q", v)
	}

	m = apply(t, m, event.RunDone{OK: true, Dur: time.Minute})
	if v := m.View().Content; v != "" {
		t.Fatalf("RunDone must collapse the live region to zero lines, got %q", v)
	}
	if m.View().AltScreen {
		t.Fatal("the live view must NEVER use the alt screen")
	}
}

// TestLiveQuitsOnStreamEnd pins the single quit path: eofMsg (the channel
// closing, which strictly follows every event) returns tea.Quit.
func TestLiveQuitsOnStreamEnd(t *testing.T) {
	m := newLiveModel(func() {})
	next, cmd := m.Update(eofMsg{})
	if cmd == nil {
		t.Fatal("eofMsg must quit the program")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("eofMsg must return tea.Quit, got %T", cmd())
	}
	if v := next.(liveModel).View().Content; v != "" {
		t.Fatalf("the region must be collapsed at quit, got %q", v)
	}
}

// TestLiveCtrlCCancelsWithoutQuitting pins §4.2 item 5: ctrl+c maps to the
// run context's cancel func and does NOT short-circuit the program — the
// producer unwinds, terminal events flow, and quit happens on stream end.
func TestLiveCtrlCCancelsWithoutQuitting(t *testing.T) {
	cancelled := false
	m := newLiveModel(func() { cancelled = true })
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if !cancelled {
		t.Fatal("ctrl+c must invoke the run context's cancel func")
	}
	if cmd != nil {
		if _, isQuit := cmd().(tea.QuitMsg); isQuit {
			t.Fatal("ctrl+c must not quit directly — the program exits through the ordinary stream-end path")
		}
	}
}

// te1Sequence is the canonical up run behind the live step-tree frame:
// four completed steps, then the enumerated packs step in flight with two
// captured log lines. Every «dynamic» span is the fixed value below.
func te1Sequence() []event.Event {
	return []event.Event{
		event.RunStarted{Cmd: "up", Cube: "voodoo"},
		event.StepDone{Stage: "config", Msg: `cube "voodoo" loaded and validated`},
		event.StepDone{Stage: "cluster", Msg: "kind cluster ready (context kind-voodoo)", Dur: 28 * time.Second},
		event.StepDone{Stage: "registry", Msg: "zot ready at zot.cube-idp-system:5000", Dur: 6 * time.Second},
		event.StepDone{Stage: "engine", Msg: "flux installed", Dur: 2 * time.Second},
		event.StepStarted{Stage: "packs", Msg: "delivering gitea@0.1.0", Index: 3, Total: 7},
		event.StepLog{Stage: "packs", Line: "fetching oci://ghcr.io/cube-idp/packs/gitea:0.1.0"},
		event.StepLog{Stage: "packs", Line: "verifying digest sha256:9f2c…"},
	}
}

// TestTE1_UpLiveFrame is the live-mode golden: scrollback lines plus the
// managed region for the canonical up sequence, ANSI-stripped, against
// testdata/te1_scrollback.golden. The golden IS the frame.
func TestTE1_UpLiveFrame(t *testing.T) {
	evs := te1Sequence()
	scroll := sbLines(newScrollback(theme.New(true)), evs...)

	m := newLiveModel(func() {})
	m.now = func() time.Time { return time.Unix(0, 0) }
	m = apply(t, m, evs...)

	got := stripANSI(strings.Join(scroll, "\n") + "\n" + m.View().Content + "\n")
	if want := readGolden(t, "te1_scrollback.golden"); got != want {
		t.Fatalf("up live frame drifted from golden:\ngot:\n%s\nwant:\n%s\ngot bytes: %q", got, want, got)
	}
}

// TestTE1_TailBounded pins the bounded tail window: seven StepLogs for the open
// stage leave exactly the LAST five lines in the region.
func TestTE1_TailBounded(t *testing.T) {
	m := newLiveModel(func() {})
	m.now = func() time.Time { return time.Unix(0, 0) }
	m = apply(t, m, event.StepStarted{Stage: "packs", Msg: "delivering gitea@0.1.0"})
	for _, l := range []string{"log1", "log2", "log3", "log4", "log5", "log6", "log7"} {
		m = apply(t, m, event.StepLog{Stage: "packs", Line: l})
	}
	v := stripANSI(m.View().Content)
	if got := strings.Count(v, "│"); got != 5 {
		t.Fatalf("tail must show exactly 5 lines, got %d:\n%s", got, v)
	}
	for _, want := range []string{"log3", "log4", "log5", "log6", "log7"} {
		if !strings.Contains(v, want) {
			t.Fatalf("tail missing %q (must keep the LAST five):\n%s", want, v)
		}
	}
	for _, gone := range []string{"log1", "log2"} {
		if strings.Contains(v, gone) {
			t.Fatalf("tail must have evicted %q:\n%s", gone, v)
		}
	}
}

// TestTE1_DurationColumn pins the right-aligned duration column:
// durations of differently-sized messages start at the same x-position.
func TestTE1_DurationColumn(t *testing.T) {
	sb := newScrollback(theme.New(true))
	long := stripANSI(sb.stepDoneLine(event.StepDone{Stage: "cluster", Msg: "kind cluster ready (context kind-voodoo)", Dur: 28 * time.Second}))
	short := stripANSI(sb.stepDoneLine(event.StepDone{Stage: "engine", Msg: "flux installed", Dur: 6 * time.Second}))
	li, si := strings.Index(long, "(28s)"), strings.Index(short, "(6s)")
	if li < 0 || si < 0 {
		t.Fatalf("durations missing:\n%q\n%q", long, short)
	}
	// Column = terminal cells before the duration (byte offsets lie: ✔ is
	// one cell, three bytes).
	lcol, scol := lipgloss.Width(long[:li]), lipgloss.Width(short[:si])
	if lcol != scol {
		t.Fatalf("durations must share a column: %d vs %d\n%q\n%q", lcol, scol, long, short)
	}
	if lcol != durCol {
		t.Fatalf("duration column pinned at %d, got %d", durCol, lcol)
	}
}

// TestTE2_StepFailedCarriesMsgDur pins the failure-line contract: the ✗ line always carries a
// message and duration — an empty Msg becomes "failed", never a naked ✗.
func TestTE2_StepFailedCarriesMsgDur(t *testing.T) {
	sb := newScrollback(theme.New(true))
	line := sb.stepFailedLine(event.StepFailed{Stage: "packs", Msg: "gitea@0.1.0 pull failed", Dur: 4 * time.Second})
	if !strings.Contains(line, sb.th.Err.Render(theme.GlyphErr)) {
		t.Fatalf("the ✗ must render via theme.Err: %q", line)
	}
	plain := stripANSI(line)
	for _, want := range []string{"✗", "[packs]", "gitea@0.1.0 pull failed", "(4s)"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("step-failed line missing %q: %q", want, plain)
		}
	}
	if empty := stripANSI(sb.stepFailedLine(event.StepFailed{Stage: "packs"})); !strings.Contains(empty, "failed") {
		t.Fatalf(`empty Msg must fall back to "failed": %q`, empty)
	}
}

// TestTE2_FailureFlushesTail is the recorded failure frame: the failed
// stage's full captured tail flushes to scrollback beneath the ✗ line —
// before the diagnosis box, which renders after program exit.
func TestTE2_FailureFlushesTail(t *testing.T) {
	sb := newScrollback(theme.New(true))
	got := sbLines(sb,
		event.StepLog{Stage: "packs", Line: "GET https://ghcr.io/v2/…/gitea/manifests/0.1.0 → 401"},
		event.StepLog{Stage: "packs", Line: "retry 1/1 failed: authentication required"},
		event.StepFailed{Stage: "packs", Msg: "gitea@0.1.0 pull failed", Dur: 4 * time.Second},
	)
	if len(got) != 3 || !strings.Contains(got[0], "✗") ||
		!strings.Contains(got[1], "401") || !strings.Contains(got[2], "retry 1/1") {
		t.Fatalf("failure must flush ✗ line then the buffered tail in order, got %q", got)
	}
	if plain := stripANSI(strings.Join(got, "\n") + "\n"); plain != readGolden(t, "te2_failure.golden") {
		t.Fatalf("failure frame drifted from golden:\ngot:\n%s\ngot bytes: %q", plain, plain)
	}
	if again := sb.lines(event.StepFailed{Stage: "packs", Msg: "x", Dur: time.Second}); len(again) != 1 {
		t.Fatalf("the tail must be consumed by the flush, got %q", again)
	}
}

// TestTE4_EpilogueGolden is the recorded success-epilogue frame: the structured
// Epilogue renders the headline + key-value rows + next-hint, and RunDone
// follows with the run summary carrying the total duration.
func TestTE4_EpilogueGolden(t *testing.T) {
	got := stripANSI(strings.Join(sbLines(newScrollback(theme.New(true)),
		event.RunStarted{Cmd: "up", Cube: "voodoo"},
		event.Epilogue{
			Cube:       "voodoo",
			GatewayURL: "https://voodoo.local:8443",
			Context:    "kind-voodoo",
			Registry:   "zot.cube-idp-system:5000",
			Hint:       "credentials: cube-idp get secrets",
		},
		event.RunDone{OK: true, Dur: 2*time.Minute + 13*time.Second},
	), "\n") + "\n")
	if want := readGolden(t, "te4_epilogue.golden"); got != want {
		t.Fatalf("epilogue frame drifted from golden:\ngot:\n%s\nwant:\n%s\ngot bytes: %q", got, want, got)
	}
}
