package render

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/cube-idp/cube-idp/internal/ui/event"
)

// JSON returns the machine projection: exactly one JSON object per line, one
// event per object — never batched, never pretty-printed (design doc §5.3;
// the moby/buildkit #4769 lesson). Every line carries "v":1 (schema version,
// EXPERIMENTAL until the D5 v1 config freeze) and "ts" (RFC3339Nano).
// Stream target is stdout; stderr stays free for the human-readable
// diagnosis block main.go still prints in JSON mode.
func JSON(w io.Writer) func(event.Event) {
	return JSONWithClock(w, time.Now)
}

// JSONWithClock is JSON with an injectable clock so golden tests can pin the
// full line bytes deterministically.
func JSONWithClock(w io.Writer, now func() time.Time) func(event.Event) {
	emit := func(v any) {
		b, err := json.Marshal(v)
		if err != nil {
			// An event that cannot marshal is a programming error in this
			// package (all fields are plain data); surface it on the stream
			// rather than silently dropping the event.
			fmt.Fprintf(w, `{"v":1,"type":"encode_error","error":%q}`+"\n", err.Error())
			return
		}
		w.Write(append(b, '\n'))
	}
	ts := func() string { return now().Format(time.RFC3339Nano) }
	return func(ev event.Event) {
		switch e := ev.(type) {
		case event.RunStarted:
			emit(jsonRunStarted{jsonHead{1, ts(), "run_started"}, e.Cmd, e.Cube})
		case event.StepStarted:
			emit(jsonStep{jsonHead{1, ts(), "step_started"}, e.Stage, e.Msg, 0})
		case event.StepDone:
			emit(jsonStep{jsonHead{1, ts(), "step_done"}, e.Stage, e.Msg, e.Dur.Milliseconds()})
		case event.StepFailed:
			emit(jsonStepFailed{jsonHead{1, ts(), "step_failed"}, e.Stage})
		case event.HealthTick:
			comps := make([]jsonComponent, len(e.Components))
			for i, c := range e.Components {
				comps[i] = jsonComponent{c.Name, c.Ready, c.Message}
			}
			emit(jsonHealthTick{jsonHead{1, ts(), "health_tick"}, comps})
		case event.Note:
			emit(jsonMsg{jsonHead{1, ts(), "note"}, e.Msg})
		case event.Warn:
			emit(jsonMsg{jsonHead{1, ts(), "warn"}, e.Msg})
		case event.Access:
			packs := make([]jsonPack, len(e.Packs))
			for i, p := range e.Packs {
				packs[i] = jsonPack{p.Name, p.URLs}
			}
			emit(jsonAccess{jsonHead{1, ts(), "access"}, packs, e.Hint})
		case event.RunDone:
			emit(jsonRunDone{jsonHead{1, ts(), "run_done"}, e.OK, e.Dur.Milliseconds()})
		case event.Diagnosis:
			d := jsonDiagnosis{jsonHead: jsonHead{1, ts(), "diagnosis"}, Raw: e.Raw}
			if e.Err != nil {
				d.Code = string(e.Err.Code)
				d.Summary = e.Err.Summary
				d.Remediation = e.Err.Remediation
				if e.Err.Cause != nil { // Cause is an error, not a string; omitted when nil
					d.Cause = e.Err.Cause.Error()
				}
			}
			emit(d)
		}
	}
}

// jsonHead is the common prefix of every stream line (field names normative,
// design doc §5.3).
type jsonHead struct {
	V    int    `json:"v"`
	TS   string `json:"ts"`
	Type string `json:"type"`
}

type jsonRunStarted struct {
	jsonHead
	Cmd  string `json:"cmd"`
	Cube string `json:"cube"`
}

type jsonStep struct {
	jsonHead
	Stage string `json:"stage"`
	Msg   string `json:"msg"`
	DurMS int64  `json:"dur_ms,omitempty"` // omitted when 0 (instantaneous steps)
}

type jsonStepFailed struct {
	jsonHead
	Stage string `json:"stage"`
}

type jsonComponent struct {
	Name    string `json:"name"`
	Ready   bool   `json:"ready"`
	Message string `json:"message"`
}

type jsonHealthTick struct {
	jsonHead
	Components []jsonComponent `json:"components"`
}

type jsonMsg struct {
	jsonHead
	Msg string `json:"msg"`
}

type jsonPack struct {
	Name string   `json:"name"`
	URLs []string `json:"urls"`
}

type jsonAccess struct {
	jsonHead
	Packs []jsonPack `json:"packs"`
	Hint  string     `json:"hint"`
}

type jsonRunDone struct {
	jsonHead
	OK    bool  `json:"ok"`
	DurMS int64 `json:"dur_ms"`
}

type jsonDiagnosis struct {
	jsonHead
	Code        string `json:"code,omitempty"`
	Summary     string `json:"summary,omitempty"`
	Cause       string `json:"cause,omitempty"`
	Remediation string `json:"remediation,omitempty"`
	Raw         string `json:"raw"`
}
