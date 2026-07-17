package ui

import (
	"bytes"
	"os"
	"testing"
	"time"
)

// The single prompt gate (spec Decision 4 + §6.3): buffers, pipes, and
// non-rich modes can NEVER prompt — and a disallowed Confirm must return
// the default without reading or writing a byte, within milliseconds.
func TestPromptsAllowedFalseForBuffers(t *testing.T) {
	SetMode(ModeStyled)
	defer SetMode(ModeStyled)
	if PromptsAllowed(&bytes.Buffer{}, &bytes.Buffer{}) {
		t.Fatal("buffers must never be promptable")
	}
}

func TestConfirmNonTTYReturnsDefaultWithoutBlocking(t *testing.T) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		in, out := &bytes.Buffer{}, &bytes.Buffer{}
		ok, err := Confirm(in, out, ConfirmOpts{Title: "?", Default: false})
		if err != nil || ok != false || out.Len() != 0 {
			t.Errorf("disallowed Confirm leaked: ok=%v err=%v wrote=%q", ok, err, out.String())
		}
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Confirm blocked on a non-TTY — the exact failure mode that hangs CI")
	}
}

func TestPromptsRefusedWhilePipelineActive(t *testing.T) {
	pipelineActive.Store(true)
	defer pipelineActive.Store(false)
	if PromptsAllowed(os.Stdin, os.Stdout) {
		t.Fatal("prompts must never share the terminal with a running pipeline (spec Decision 5)")
	}
}
