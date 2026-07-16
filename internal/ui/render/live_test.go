package render

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/cube-idp/cube-idp/internal/ui/event"
)

// TestScrollbackLineContentIdentical pins the §5.2 content-identical rule:
// styled scrollback lines carry the same stage tags and words as the plain
// projection — presentation (glyph, color, duration suffix) only.
func TestScrollbackLineContentIdentical(t *testing.T) {
	done := scrollbackLine(event.StepDone{Stage: "cluster", Msg: "kind cluster ready (context kind-dev)", Dur: 12 * time.Second})
	for _, want := range []string{"✔", "[cluster]", "kind cluster ready (context kind-dev)", "(12s)"} {
		if !strings.Contains(done, want) {
			t.Fatalf("StepDone scrollback missing %q: %q", want, done)
		}
	}
	if instant := scrollbackLine(event.StepDone{Stage: "tls", Msg: "ready"}); strings.Contains(instant, "(") {
		t.Fatalf("Dur==0 must not print a duration: %q", instant)
	}

	failed := scrollbackLine(event.StepFailed{Stage: "engine"})
	for _, want := range []string{"✗", "[engine]"} {
		if !strings.Contains(failed, want) {
			t.Fatalf("StepFailed scrollback missing %q: %q", want, failed)
		}
	}

	const noteMsg = "\n✔ cube \"dev\" is up — https://cube.local:8443\n  credentials: cube-idp get secrets"
	if got := scrollbackLine(event.Note{Msg: noteMsg}); got != noteMsg {
		t.Fatalf("Note must pass through verbatim:\ngot:  %q\nwant: %q", got, noteMsg)
	}

	if warn := scrollbackLine(event.Warn{Msg: "heads up"}); !strings.Contains(warn, "⚠") || !strings.Contains(warn, "heads up") {
		t.Fatalf("Warn scrollback drifted: %q", warn)
	}

	access := scrollbackLine(event.Access{
		Packs: []event.PackAccess{{Name: "gitea", URLs: []string{"https://gitea.cube.local:8443"}}},
		Hint:  "credentials: cube-idp get secrets",
	})
	for _, want := range []string{"Access", "gitea", "https://gitea.cube.local:8443", "credentials: cube-idp get secrets"} {
		if !strings.Contains(access, want) {
			t.Fatalf("Access scrollback missing %q: %q", want, access)
		}
	}
}

// TestScrollbackSilentEvents pins that region-only events never print to
// scrollback — the Diagnosis in particular renders AFTER program exit via
// main.go's ui.RenderError, never here (diagnosis-last, §5.2).
func TestScrollbackSilentEvents(t *testing.T) {
	silent := []event.Event{
		event.RunStarted{Cmd: "up", Cube: "dev"},
		event.StepStarted{Stage: "cluster", Msg: "creating"},
		event.HealthTick{Components: []event.ComponentState{{Name: "x"}}},
		event.RunDone{OK: false, Dur: time.Second},
		event.Diagnosis{Raw: "boom"},
	}
	for _, ev := range silent {
		if got := scrollbackLine(ev); got != "" {
			t.Fatalf("%T must not reach scrollback, got %q", ev, got)
		}
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
// resolution; the health table exists only while the health stage is open;
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
	if v := m.View().Content; strings.Contains(v, "cube-idp-traefik") {
		t.Fatalf("health table must collapse with its stage: %q", v)
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
