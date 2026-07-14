package ui

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/rafpe/cube-idp/internal/diag"
	"github.com/rafpe/cube-idp/internal/ui/event"
	"github.com/rafpe/cube-idp/internal/ui/render"
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

	switch {
	case mode == ModeJSON:
		drain(ch, render.JSON(out))
	case mode == ModeLive || (mode == ModeStyled && IsTerminal(out)):
		runLive(out, cancel, ch)
	default: // ModePlain, or auto-styled downgraded per-writer
		drain(ch, render.Plain(out))
	}

	// The renderer saw the channel close, so the producer goroutine has
	// already sent its error; this join is immediate and guarantees no
	// goroutine survives RunPipeline.
	return <-errCh
}

// runLive runs the LiveRenderer. Placeholder until the Bubble Tea v2 inline
// program lands (Task 14b step 4): projects plain so the pipeline stays
// correct end-to-end in every mode meanwhile.
func runLive(out io.Writer, cancel context.CancelFunc, ch <-chan event.Event) {
	_ = cancel
	drain(ch, render.Plain(out))
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
