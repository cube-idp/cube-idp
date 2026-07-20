package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/cube-idp/cube-idp/cmd"
	"github.com/cube-idp/cube-idp/internal/ui"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := cmd.Execute(ctx); err != nil {
		// A plugin's own non-zero exit propagates verbatim, unrendered —
		// its output is its diagnosis — wrapping it would hide the plugin's
		// own error text behind a cube-idp frame. Everything else
		// renders as a CUBE-xxxx block and exits 1, as before.
		code, render := cmd.ExitCodeFor(err)
		if render {
			// ui.RenderErrorTo: diag.Render verbatim in plain/JSON modes or
			// whenever stderr is not a real terminal (a `2>file` redirect
			// must never capture ANSI borders); a styled panel on a rich
			// terminal. Printed only after Execute returned — i.e. after any
			// live program fully released the terminal — so the diagnosis is
			// always the last thing shown (the diagnosis-last rule).
			fmt.Fprintln(os.Stderr, ui.RenderErrorTo(os.Stderr, err))
		}
		stop() // os.Exit skips deferred calls; release the signal handler explicitly
		os.Exit(code)
	}
}
