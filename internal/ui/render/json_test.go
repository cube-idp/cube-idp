package render

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/rafpe/cube-idp/internal/ui/event"
)

func diagRawOnly() event.Diagnosis { return event.Diagnosis{Raw: "plain failure"} }

func fixedClock() func() time.Time {
	t0 := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	return func() time.Time { return t0 }
}

// TestJSONGoldenUpRun pins the machine stream: one JSON object per line, one
// event per object, every line carrying "v":1 and "ts" (design doc §5.3;
// field names normative; step_done dur_ms omitted when 0).
func TestJSONGoldenUpRun(t *testing.T) {
	var b bytes.Buffer
	project(t, canonicalUpRun(), JSONWithClock(&b, fixedClock()))

	const ts = `"ts":"2026-07-14T12:00:00Z"`
	wantLines := []string{
		`{"v":1,` + ts + `,"type":"run_started","cmd":"up","cube":"dev"}`,
		`{"v":1,` + ts + `,"type":"step_done","stage":"config","msg":"cube \"dev\" loaded and validated"}`,
		`{"v":1,` + ts + `,"type":"step_done","stage":"ca","msg":"local CA ready (/home/u/.config/cube-idp/ca.crt)"}`,
		`{"v":1,` + ts + `,"type":"step_started","stage":"cluster","msg":"creating kind cluster"}`,
		`{"v":1,` + ts + `,"type":"step_done","stage":"cluster","msg":"kind cluster ready (context kind-dev)","dur_ms":72340}`,
		`{"v":1,` + ts + `,"type":"step_done","stage":"registry","msg":"zot ready at http://zot.zot.svc.cluster.local:5000"}`,
		`{"v":1,` + ts + `,"type":"step_done","stage":"packs-crd","msg":"Pack CRD established"}`,
		`{"v":1,` + ts + `,"type":"step_started","stage":"engine","msg":"installing flux"}`,
		`{"v":1,` + ts + `,"type":"step_done","stage":"engine","msg":"flux installed","dur_ms":30000}`,
		"{\"v\":1," + ts + ",\"type\":\"step_done\",\"stage\":\"tls\",\"msg\":\"gateway certificate ready (CA: run `cube-idp trust` to make browsers trust it)\"}",
		`{"v":1,` + ts + `,"type":"step_done","stage":"pack","msg":"traefik@0.1.0 delivered"}`,
		`{"v":1,` + ts + `,"type":"step_done","stage":"pack","msg":"gitea@0.1.0 delivered"}`,
		`{"v":1,` + ts + `,"type":"step_done","stage":"lock","msg":"cube.lock written (2 packs)"}`,
		`{"v":1,` + ts + `,"type":"step_done","stage":"dns","msg":"*.cube.local resolves to the gateway in-cluster"}`,
		`{"v":1,` + ts + `,"type":"step_started","stage":"health","msg":"waiting for components to become ready"}`,
		`{"v":1,` + ts + `,"type":"health_tick","components":[{"name":"cube-idp-traefik","ready":false,"message":"reconciling"},{"name":"cube-idp-gitea","ready":false,"message":"reconciling"}]}`,
		`{"v":1,` + ts + `,"type":"health_tick","components":[{"name":"cube-idp-traefik","ready":true,"message":"Applied revision"},{"name":"cube-idp-gitea","ready":true,"message":"Applied revision"}]}`,
		`{"v":1,` + ts + `,"type":"step_done","stage":"health","msg":"3 component(s) ready","dur_ms":45000}`,
		`{"v":1,` + ts + `,"type":"step_done","stage":"packs","msg":"2 pack records written — try ` + "`kubectl get packs`" + `"}`,
		`{"v":1,` + ts + `,"type":"note","msg":"\n✔ cube \"dev\" is up — https://cube.local:8443\n  credentials: cube-idp get secrets"}`,
		`{"v":1,` + ts + `,"type":"access","packs":[{"name":"gitea","urls":["https://gitea.cube.local:8443"]}],"hint":"credentials: cube-idp get secrets"}`,
		`{"v":1,` + ts + `,"type":"run_done","ok":true,"dur_ms":180000}`,
	}

	got := strings.Split(strings.TrimRight(b.String(), "\n"), "\n")
	if len(got) != len(wantLines) {
		t.Fatalf("want %d lines (one event per line, never batched), got %d:\n%s",
			len(wantLines), len(got), b.String())
	}
	for i, want := range wantLines {
		if got[i] != want {
			t.Fatalf("line %d drifted:\ngot:  %s\nwant: %s", i+1, got[i], want)
		}
	}
}

// TestJSONFailedRunDiagnosisLast pins the failure contract: diagnosis is the
// FINAL line (Terraform's diagnostic precedent — machine consumers may treat
// it as the terminal record), carries code/summary/cause/remediation from
// the typed error, and raw is always set.
func TestJSONFailedRunDiagnosisLast(t *testing.T) {
	var b bytes.Buffer
	project(t, failedRun(), JSONWithClock(&b, fixedClock()))

	lines := strings.Split(strings.TrimRight(b.String(), "\n"), "\n")
	last := lines[len(lines)-1]

	var d struct {
		V           int    `json:"v"`
		Type        string `json:"type"`
		Code        string `json:"code"`
		Summary     string `json:"summary"`
		Cause       string `json:"cause"`
		Remediation string `json:"remediation"`
		Raw         string `json:"raw"`
	}
	if err := json.Unmarshal([]byte(last), &d); err != nil {
		t.Fatalf("last line is not a JSON object: %v\n%s", err, last)
	}
	if d.Type != "diagnosis" {
		t.Fatalf("diagnosis must be the final line on failure, got type %q:\n%s", d.Type, b.String())
	}
	if d.V != 1 || d.Code != "CUBE-1001" || d.Summary != "kind cluster create failed" ||
		d.Cause != "docker not running" || d.Remediation == "" || d.Raw == "" {
		t.Fatalf("diagnosis fields drifted: %+v", d)
	}
	// step_failed precedes run_done{ok:false} precedes diagnosis.
	joined := b.String()
	fi := strings.Index(joined, `"type":"step_failed"`)
	ri := strings.Index(joined, `"type":"run_done"`)
	di := strings.Index(joined, `"type":"diagnosis"`)
	if !(fi >= 0 && ri > fi && di > ri) {
		t.Fatalf("failure ordering must be step_failed -> run_done -> diagnosis:\n%s", joined)
	}
	if !strings.Contains(joined, `"ok":false`) {
		t.Fatalf("run_done must carry ok:false on failure:\n%s", joined)
	}
}

// TestJSONDiagnosisUntypedError covers the fallback: no *diag.Error means
// code/summary/cause/remediation are omitted entirely and raw carries the
// error text.
func TestJSONDiagnosisUntypedError(t *testing.T) {
	var b bytes.Buffer
	JSONWithClock(&b, fixedClock())(diagRawOnly())
	const want = `{"v":1,"ts":"2026-07-14T12:00:00Z","type":"diagnosis","raw":"plain failure"}` + "\n"
	if got := b.String(); got != want {
		t.Fatalf("untyped diagnosis drifted:\ngot:  %swant: %s", got, want)
	}
}
