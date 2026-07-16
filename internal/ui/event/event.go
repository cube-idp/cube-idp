// Package event defines the renderer-agnostic vocabulary of everything a
// cube-idp run can tell a user. Renderers project these events; they never
// invent content (spec D13, BuildKit's SolveStatus precedent).
//
// Ordering rules (normative, design doc §3):
//
//  1. RunStarted first, when emitted at all (skipped if config.Load fails).
//  2. Every StepStarted is resolved by the next StepDone or StepFailed for
//     the same stage, or implicitly by RunDone (renderers MUST treat
//     RunDone/Diagnosis as resolving any still-open step).
//  3. Success termination: ... → Access? → RunDone{OK:true, Dur}. Nothing
//     follows RunDone on success.
//  4. Failure termination: ... → StepFailed? → RunDone{OK:false, Dur} →
//     Diagnosis. Diagnosis is always the final event on failure — machine
//     consumers may treat it as the terminal record (Terraform's
//     `diagnostic` precedent).
//
// Stage names are today's badge names — the exact strings already passed to
// step()/Progress in internal/up/up.go ("config", "ca", "cluster",
// "registry", "packs-crd", "engine", "tls", "pack", "lock", "dns",
// "health", "packs", plus "cnoe" from cmd/cnoe.go). down introduces
// "engine", "dns", "cascade", "cluster", "trust". Stage is an open string,
// not an enum — packs and future commands add stages without touching this
// package.
package event

import (
	"time"

	"github.com/cube-idp/cube-idp/internal/diag"
)

// Event is the closed set of run events. The marker method keeps the set
// closed at compile time (a renderer switch over these types is exhaustive
// by convention; new events require touching every renderer).
type Event interface{ event() }

// RunStarted opens a run. Cube is the cube name from cube.yaml; emitted by
// the producer immediately after config.Load succeeds (it is not emitted at
// all when config loading fails — consumers must tolerate a stream that is
// only RunDone+Diagnosis).
type RunStarted struct{ Cmd, Cube string }

// StepStarted marks a stage as in-flight. Today's ui.Progress start.
// Plain projection: zero bytes (the pinned Progress invariant).
type StepStarted struct{ Stage, Msg string }

// StepDone completes a stage. Today's ui.Printer.Step / Progress.Done.
// Dur is 0 for instantaneous steps (plain projection never includes it).
type StepDone struct {
	Stage, Msg string
	Dur        time.Duration
}

// StepFailed marks the in-flight stage as failed. Today's Progress.Stop on
// an error path. Err is nil when the authoritative error arrives later as
// Diagnosis (the common case: the producer's error unwinds to the run
// lifecycle, which emits Diagnosis).
type StepFailed struct {
	Stage string
	Err   *diag.Error
}

// ComponentState mirrors engine.ComponentHealth (internal/engine/engine.go)
// without importing it (event stays dependency-light).
type ComponentState struct {
	Name    string
	Ready   bool
	Message string
}

// HealthTick carries one waitHealthy poll result. Emitted on the FIRST poll
// and thereafter only when any component's Ready/Message changed — keeps the
// JSON stream from repeating identical lines every 5s.
type HealthTick struct{ Components []ComponentState }

// Note is a neutral passthrough line (e.g. up's final success block, down's
// trust-revert messages). Msg carries any embedded newlines; renderers add
// exactly one trailing newline.
type Note struct{ Msg string }

// Warn is an advisory (e.g. get secrets' legacy-label deprecation note).
type Warn struct{ Msg string }

// PackAccess is one delivered pack's access info (today's ui.PackAccess —
// internal/ui keeps `type PackAccess = event.PackAccess` as an alias so
// internal/up's construction sites don't churn).
type PackAccess struct {
	Name string
	URLs []string
}

// Access is the post-up "here's what you just got" summary. See design doc
// §9: as of stage A this HAS a plain projection (the one deliberate plain
// change).
type Access struct {
	Packs []PackAccess
	Hint  string
}

// Diagnosis is ALWAYS the last event on a failed run. Err is the typed
// CUBE-xxxx error when errors.As finds one; Raw is err.Error() and is
// always set (the fallback for untyped errors).
type Diagnosis struct {
	Err *diag.Error
	Raw string
}

// RunDone closes a run. On failure it is emitted immediately BEFORE
// Diagnosis (so Diagnosis stays terminal).
type RunDone struct {
	OK  bool
	Dur time.Duration
}

func (RunStarted) event()  {}
func (StepStarted) event() {}
func (StepDone) event()    {}
func (StepFailed) event()  {}
func (HealthTick) event()  {}
func (Note) event()        {}
func (Warn) event()        {}
func (Access) event()      {}
func (Diagnosis) event()   {}
func (RunDone) event()     {}
