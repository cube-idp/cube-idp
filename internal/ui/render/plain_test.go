package render

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/cube-idp/cube-idp/internal/diag"
	"github.com/cube-idp/cube-idp/internal/ui/event"
)

// canonicalUpRun is the recorded event slice of a full successful `up`,
// exercising every event type the success path can emit — the §12
// golden-stream fixture. Its plain projection must be byte-identical to
// what the pre-Task-14b code emitted for the same run
// (testdata/plain_up_pretask.golden, recorded from the pre-task tree)
// plus ONLY the owner-ratified Access block (§9) and the R2 one-glyph
// change (the epilogue's leading "✔ " moved from content to presentation).
func canonicalUpRun() []event.Event {
	return []event.Event{
		event.RunStarted{Cmd: "up", Cube: "dev"},
		event.StepDone{Stage: "config", Msg: `cube "dev" loaded and validated`},
		event.StepDone{Stage: "ca", Msg: "local CA ready (/home/u/.config/cube-idp/ca.crt)"},
		event.StepStarted{Stage: "cluster", Msg: "creating kind cluster"},
		event.StepDone{Stage: "cluster", Msg: "kind cluster ready (context kind-dev)", Dur: 72340 * time.Millisecond},
		event.StepDone{Stage: "registry", Msg: "zot ready at http://zot.zot.svc.cluster.local:5000"},
		event.StepDone{Stage: "packs-crd", Msg: "Pack CRD established"},
		event.StepStarted{Stage: "engine", Msg: "installing flux"},
		event.StepDone{Stage: "engine", Msg: "flux installed", Dur: 30 * time.Second},
		event.StepDone{Stage: "tls", Msg: "gateway certificate ready (CA: run `cube-idp trust` to make browsers trust it)"},
		event.StepDone{Stage: "pack", Msg: "traefik@0.1.0 delivered"},
		event.StepDone{Stage: "pack", Msg: "gitea@0.1.0 delivered"},
		event.StepDone{Stage: "lock", Msg: "cube.lock written (2 packs)"},
		event.StepDone{Stage: "dns", Msg: "*.cube.local resolves to the gateway in-cluster"},
		event.StepStarted{Stage: "health", Msg: "waiting for components to become ready"},
		event.HealthTick{Components: []event.ComponentState{
			{Name: "cube-idp-traefik", Ready: false, Message: "reconciling"},
			{Name: "cube-idp-gitea", Ready: false, Message: "reconciling"},
		}},
		event.HealthTick{Components: []event.ComponentState{
			{Name: "cube-idp-traefik", Ready: true, Message: "Applied revision"},
			{Name: "cube-idp-gitea", Ready: true, Message: "Applied revision"},
		}},
		event.StepDone{Stage: "health", Msg: "3 component(s) ready", Dur: 45 * time.Second},
		event.StepDone{Stage: "packs", Msg: "2 pack records written — try `kubectl get packs`"},
		event.Epilogue{
			Cube: "dev", GatewayURL: "https://cube.local:8443",
			Context: "kind-dev", Registry: "zot.cube-idp-system.svc.cluster.local:5000",
			Hint: "credentials: cube-idp get secrets",
		},
		event.Access{
			Packs: []event.PackAccess{{Name: "gitea", URLs: []string{"https://gitea.cube.local:8443"}}},
			Hint:  "credentials: cube-idp get secrets",
		},
		event.RunDone{OK: true, Dur: 3 * time.Minute},
	}
}

// failedRun is the recorded failure fixture: config+ca succeed, the cluster
// step opens and fails, and the run terminates with the normative
// StepFailed → RunDone{false} → Diagnosis ordering.
func failedRun() []event.Event {
	return []event.Event{
		event.RunStarted{Cmd: "up", Cube: "dev"},
		event.StepDone{Stage: "config", Msg: `cube "dev" loaded and validated`},
		event.StepDone{Stage: "ca", Msg: "local CA ready (/home/u/.config/cube-idp/ca.crt)"},
		event.StepStarted{Stage: "cluster", Msg: "creating kind cluster"},
		event.StepFailed{Stage: "cluster"},
		event.RunDone{OK: false, Dur: 90 * time.Second},
		event.Diagnosis{
			Err: diag.Wrap(errors.New("docker not running"), diag.Code("CUBE-1001"),
				"kind cluster create failed", "start docker and re-run `cube-idp up`"),
			Raw: "CUBE-1001: kind cluster create failed: docker not running",
		},
	}
}

func project(t *testing.T, evs []event.Event, r func(event.Event)) {
	t.Helper()
	for _, ev := range evs {
		r(ev)
	}
}

// TestPlainGoldenUpRun is Task 14b's byte-neutrality proof for `up | cat`:
// the plain projection of the canonical up stream must equal the bytes the
// pre-task code emitted for the same run (recorded golden) plus exactly the
// owner-ratified Access block — nothing else may differ (design doc §8/§9).
func TestPlainGoldenUpRun(t *testing.T) {
	pretask, err := os.ReadFile("testdata/plain_up_pretask.golden")
	if err != nil {
		t.Fatal(err)
	}
	const accessBlock = "\nAccess\n" +
		"  gitea        https://gitea.cube.local:8443\n" +
		"  credentials: cube-idp get secrets\n"
	// R2 (ratified, spec §5): the plain bytes differ from the pre-task
	// recording by EXACTLY the epilogue's leading "✔ " — the glyph moved
	// from content to presentation. The golden keeps the historical bytes;
	// this transform is the entire ratified diff.
	want := strings.Replace(string(pretask), "✔ ", "", 1) + accessBlock

	var b bytes.Buffer
	project(t, canonicalUpRun(), Plain(&b))
	got := b.String()
	if got != want {
		t.Fatalf("plain projection drifted from the pre-task bytes (+Access):\ngot:\n%q\nwant:\n%q", got, want)
	}
	if strings.Contains(got, "\x1b[") {
		t.Fatal("plain mode must emit zero ANSI escapes")
	}
}

// TestPlainGoldenFailedRun pins the failure projection: identical to the
// pre-task bytes (the two completed step lines), because
// StepStarted/StepFailed/RunDone/Diagnosis all project to zero plain bytes —
// the diagnosis block belongs to main.go's stderr print, not the renderer.
func TestPlainGoldenFailedRun(t *testing.T) {
	want, err := os.ReadFile("testdata/plain_fail_pretask.golden")
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	project(t, failedRun(), Plain(&b))
	if got := b.String(); got != string(want) {
		t.Fatalf("failed-run plain projection drifted:\ngot:\n%q\nwant:\n%q", got, want)
	}
}

// silentEventsFixture is the recorded slice of events that print zero bytes
// in both Plain and Styled (RunStarted/StepStarted/StepFailed/HealthTick/
// Diagnosis/RunDone) — shared by TestPlainSilentEvents and
// TestStyledSilentEventsAreZeroBytes (styled_test.go).
func silentEventsFixture() []event.Event {
	return []event.Event{
		event.RunStarted{Cmd: "up", Cube: "dev"},
		event.StepStarted{Stage: "cluster", Msg: "creating kind cluster"},
		event.StepFailed{Stage: "cluster"},
		event.HealthTick{Components: []event.ComponentState{{Name: "x", Ready: false, Message: "m"}}},
		event.Diagnosis{Raw: "boom"},
		event.RunDone{OK: false, Dur: time.Second},
	}
}

// TestPlainSilentEvents restates the pinned Progress invariants as the
// renderer contract: every event that printed nothing before Task 14b
// still projects to zero bytes.
func TestPlainSilentEvents(t *testing.T) {
	for _, ev := range silentEventsFixture() {
		var b bytes.Buffer
		Plain(&b)(ev)
		if b.Len() != 0 {
			t.Fatalf("%T must project to zero plain bytes, got %q", ev, b.String())
		}
	}
}

// R2 (spec §5): the epilogue is data; plain projects it WITHOUT the glyph.
// These bytes are the new frozen contract for event.Epilogue. (Name is
// normative — spec §6.1 matrix.)
func TestTE4_PlainBytesR2Only(t *testing.T) {
	var buf bytes.Buffer
	Plain(&buf)(event.Epilogue{
		Cube: "voodoo", GatewayURL: "https://voodoo.local:8443",
		Hint: "credentials: cube-idp get secrets",
	})
	want := "\ncube \"voodoo\" is up — https://voodoo.local:8443\n  credentials: cube-idp get secrets\n"
	if buf.String() != want {
		t.Fatalf("epilogue plain bytes:\n got %q\nwant %q", buf.String(), want)
	}
}
