package ui

import (
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/cube-idp/cube-idp/internal/ui/event"
)

// Console is the producer's handle on the event stream (design doc §4.3).
// Its method set deliberately mirrors ui.Printer so orchestrator call sites
// migrate mechanically: step(out, ...) -> con.Step(...), p.Progress(...) ->
// con.Progress(...), raw fmt.Fprintf -> con.Note(...). Console constructs
// events; it never renders — exactly one renderer (chosen by RunPipeline)
// projects them.
type Console struct {
	ch chan<- event.Event

	mu         sync.Mutex
	openStage  string                 // the unresolved StepStarted's stage, if any
	openMsg    string                 // that step's message, forwarded on unwind StepFailed
	lastHealth []event.ComponentState // change filter for Health
}

// Start emits RunStarted. Producers call it immediately after config.Load
// succeeds (never before — a failed load emits no RunStarted at all).
func (c *Console) Start(cmd, cube string) {
	c.ch <- event.RunStarted{Cmd: cmd, Cube: cube}
}

// Step emits StepDone{Dur: 0} — an instantaneous step, today's
// Printer.Step. The plain projection never includes a duration.
func (c *Console) Step(stage, format string, args ...any) {
	c.ch <- event.StepDone{Stage: stage, Msg: fmt.Sprintf(format, args...)}
}

// Progress emits StepStarted and returns the handle that resolves it —
// mirroring ui.Progress's contract exactly: every Progress is resolved by
// exactly one Done (success) or Stop (abandoned on error).
func (c *Console) Progress(stage, message string) *ConsoleProgress {
	return c.ProgressN(stage, message, 0, 0)
}

// ProgressN is Progress for enumerated repeats (pack 3/7): Index/Total ride
// StepStarted and the eventual StepDone so renderers can show n-of-m.
func (c *Console) ProgressN(stage, message string, index, total int) *ConsoleProgress {
	c.mu.Lock()
	c.openStage, c.openMsg = stage, message
	c.mu.Unlock()
	c.ch <- event.StepStarted{Stage: stage, Msg: message, Index: index, Total: total}
	return &ConsoleProgress{con: c, stage: stage, msg: message, idx: index, total: total, start: time.Now()}
}

// Log forwards one line of the open step's output (live-only tail).
func (c *Console) Log(stage, format string, args ...any) {
	c.ch <- event.StepLog{Stage: stage, Line: fmt.Sprintf(format, args...)}
}

// Note emits a neutral passthrough line. Msg may carry embedded newlines;
// renderers add exactly one trailing newline, so producers migrating a raw
// fmt.Fprintf pass the message WITHOUT its trailing \n for byte-identity.
func (c *Console) Note(format string, args ...any) {
	c.ch <- event.Note{Msg: fmt.Sprintf(format, args...)}
}

// Warn emits an advisory line.
func (c *Console) Warn(format string, args ...any) {
	c.ch <- event.Warn{Msg: fmt.Sprintf(format, args...)}
}

// Health emits a HealthTick — change-filtered: the first poll always
// emits; subsequent identical polls emit nothing, keeping the JSON stream
// from repeating identical lines every 5s.
func (c *Console) Health(components []event.ComponentState) {
	c.mu.Lock()
	same := c.lastHealth != nil && slices.Equal(c.lastHealth, components)
	if !same {
		c.lastHealth = slices.Clone(components)
	}
	c.mu.Unlock()
	if same {
		return
	}
	c.ch <- event.HealthTick{Components: components}
}

// Access emits the post-up access summary event.
func (c *Console) Access(packs []event.PackAccess, hint string) {
	c.ch <- event.Access{Packs: packs, Hint: hint}
}

// open returns the stage and message of the still-unresolved Progress, if
// any — RunPipeline emits its StepFailed when the producer's error unwinds
// past an open step without Stop being reached.
func (c *Console) open() (stage, msg string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.openStage, c.openMsg
}

func (c *Console) resolve(stage string) {
	c.mu.Lock()
	if c.openStage == stage {
		c.openStage, c.openMsg = "", ""
	}
	c.mu.Unlock()
}

// ConsoleProgress mirrors ui.Progress's resolution contract exactly: every
// Progress is resolved by exactly one Done (success) or Stop (abandoned on
// error). Done emits StepDone{Dur: since start}; Stop emits
// StepFailed{Stage, Err: nil} (the authoritative error arrives later as the
// run's Diagnosis).
type ConsoleProgress struct {
	con        *Console
	stage, msg string
	idx, total int
	start      time.Time
	resolved   bool
}

// Done resolves the step successfully. Idempotent after Stop/Done (matching
// ui.Progress's "a second Stop/Done call is a no-op").
func (cp *ConsoleProgress) Done(format string, args ...any) {
	if cp.resolved {
		return
	}
	cp.resolved = true
	cp.con.resolve(cp.stage)
	cp.con.ch <- event.StepDone{
		Stage: cp.stage,
		Msg:   fmt.Sprintf(format, args...),
		Dur:   time.Since(cp.start),
		Index: cp.idx,
		Total: cp.total,
	}
}

// Stop abandons the step on an error path. Plain projection: zero bytes,
// same as ui.Progress.Stop. It forwards the message and elapsed the handle
// already holds — a bare StepFailed{Stage} tells renderers nothing.
func (cp *ConsoleProgress) Stop() {
	if cp.resolved {
		return
	}
	cp.resolved = true
	cp.con.resolve(cp.stage)
	cp.con.ch <- event.StepFailed{Stage: cp.stage, Msg: cp.msg, Dur: time.Since(cp.start)}
}
