package ui

import (
	"context"
	"errors"
	"io"
	"os"
	"time"

	"github.com/cube-idp/cube-idp/internal/diag"
	"github.com/cube-idp/cube-idp/internal/ui/event"
	"github.com/cube-idp/cube-idp/internal/ui/render"
)

// eventBuffer is the channel capacity: a full `up` emits well under 100
// events even with health ticks; renderers consume promptly (one Fprintf or
// one tea.Program.Send per event), so the producer effectively never blocks
// on UI (design doc §4.1).
const eventBuffer = 256

// RunPipeline owns one command's event pipeline: it builds the renderer for
// the resolved Mode, hands the producer a Console, and guarantees that by
// the time it returns (a) the terminal is fully released (the bubbletea
// program, if any, has exited) and (b) no goroutine it started survives.
// It returns exactly the producer's error, so cobra/main.go error handling
// is unchanged.
//
// Renderer resolution (§4.2): ModeJSON → JSONRenderer; ModeLive →
// LiveRenderer (explicit force, even on a non-TTY); ModeStyled →
// LiveRenderer when out is also a real TTY, else PlainRenderer; ModePlain →
// PlainRenderer.
//
// Lifecycle: the producer fn runs on its own goroutine; the renderer runs
// on the calling goroutine (bubbletea input handling wants the foreground).
// When fn returns, the terminal events are emitted in the normative order —
// on success RunDone{OK:true}; on failure StepFailed for the still-open
// stage (if any), RunDone{OK:false}, then Diagnosis, ALWAYS last — and the
// channel closes, which ends the renderer's receive loop (and quits the
// live program). Ctrl-C flows through the command context (main.go's
// signal.NotifyContext) and, in live mode, through the program's ctrl+c
// mapping to the same cancel func — the producer unwinds through its normal
// error paths and the program quits through the single ordinary path,
// never via os.Exit.
func RunPipeline(ctx context.Context, cmdName string, out io.Writer,
	fn func(ctx context.Context, con *Console) error) error {
	return runPipeline(ctx, cmdName, out, fn, pickRenderer)
}

// RunPipelineStatic is RunPipeline for short, static commands (plugin, pack
// push, repo create, sync one-shot): identical lifecycle and terminal-event
// ordering, but a TTY under ModeStyled gets the Styled projection instead of
// the Live renderer — the live step-tree is reserved for long-running
// commands (vendor, up, down; UX spec §5.2 + Phase 4 spec §5.3).
// ModeLive (explicit user force) still runs the LiveRenderer; ModeJSON and
// plain behave exactly as RunPipeline.
func RunPipelineStatic(ctx context.Context, cmdName string, out io.Writer,
	fn func(ctx context.Context, con *Console) error) error {
	return runPipeline(ctx, cmdName, out, fn, pickRendererStatic)
}

// rendererPicker chooses how a resolved mode projects events for a given
// writer — the only difference between RunPipeline and RunPipelineStatic.
type rendererPicker func(mode Mode, out io.Writer, cancel context.CancelFunc, ch <-chan event.Event)

// pickRenderer is RunPipeline's renderer switch (§4.2): ModeJSON → JSON;
// ModeLive or (ModeStyled on a real TTY) → the LiveRenderer; else → Plain.
func pickRenderer(mode Mode, out io.Writer, cancel context.CancelFunc, ch <-chan event.Event) {
	switch {
	case mode == ModeJSON:
		drain(ch, render.JSON(out))
	case mode == ModeLive || (mode == ModeStyled && IsTerminal(out)):
		runLive(out, cancel, ch)
	default: // ModePlain, or auto-styled downgraded per-writer
		drain(ch, render.Plain(out))
	}
}

// pickRendererStatic is RunPipelineStatic's renderer switch: identical to
// pickRenderer except ModeStyled on a real TTY gets the Styled projection
// instead of the LiveRenderer — short static commands never pop a live
// step-tree. ModeLive is still an explicit force and still goes live.
func pickRendererStatic(mode Mode, out io.Writer, cancel context.CancelFunc, ch <-chan event.Event) {
	switch {
	case mode == ModeJSON:
		drain(ch, render.JSON(out))
	case mode == ModeLive:
		runLive(out, cancel, ch)
	case mode == ModeStyled && IsTerminal(out):
		drain(ch, render.Styled(out))
	default: // ModePlain, or auto-styled downgraded per-writer
		drain(ch, render.Plain(out))
	}
}

// runPipeline is RunPipeline/RunPipelineStatic's shared producer/lifecycle
// body — the two exported functions differ only in which rendererPicker
// they pass.
func runPipeline(ctx context.Context, cmdName string, out io.Writer,
	fn func(ctx context.Context, con *Console) error, pick rendererPicker) error {
	_ = cmdName // producers self-identify via Console.Start; kept for §4.2's normative signature

	mode := CurrentMode()
	ch := make(chan event.Event, eventBuffer)
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	con := &Console{ch: ch}
	start := time.Now()
	errCh := make(chan error, 1)
	go func() {
		err := fn(runCtx, con)
		if err != nil {
			if st := con.open(); st != "" {
				// The producer's error unwound past an open step without
				// reaching its Stop — resolve it so renderers never leave a
				// spinner dangling.
				ch <- event.StepFailed{Stage: st}
			}
			ch <- event.RunDone{OK: false, Dur: time.Since(start)}
			ch <- diagnosisOf(err) // ALWAYS the final event on failure
		} else {
			ch <- event.RunDone{OK: true, Dur: time.Since(start)}
		}
		close(ch)
		errCh <- err
	}()

	pick(mode, out, cancel, ch)

	// The renderer saw the channel close, so the producer goroutine has
	// already sent its error; this join is immediate and guarantees no
	// goroutine survives RunPipeline/RunPipelineStatic.
	return <-errCh
}

// runLive runs the Bubble Tea v2 inline LiveRenderer on the calling
// goroutine. Interactive input engages only when stdin is a real terminal
// (nil disables it entirely — ModeLive-forced runs on pipes and tests stay
// deterministic); ctrl+c maps to the run context's cancel func so the
// producer unwinds through its normal error paths (§4.2 item 5).
func runLive(out io.Writer, cancel context.CancelFunc, ch <-chan event.Event) {
	var input io.Reader
	if IsTerminal(os.Stdin) {
		input = os.Stdin
	}
	render.Live(out, input, cancel, ch)
}

// drain is the plain/JSON receive loop: one projection per event until the
// run lifecycle closes the channel.
func drain(ch <-chan event.Event, project func(event.Event)) {
	for ev := range ch {
		project(ev)
	}
}

// diagnosisOf builds the terminal Diagnosis event: the typed CUBE-xxxx
// error when errors.As finds one; Raw always set (the untyped fallback).
func diagnosisOf(err error) event.Diagnosis {
	d := event.Diagnosis{Raw: err.Error()}
	var de *diag.Error
	if errors.As(err, &de) {
		d.Err = de
	}
	return d
}
