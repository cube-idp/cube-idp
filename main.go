package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/rafpe/cube-idp/cmd"
	"github.com/rafpe/cube-idp/internal/ui"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := cmd.Execute(ctx); err != nil {
		// A plugin's own non-zero exit propagates verbatim, unrendered —
		// its output is its diagnosis (spec §4.4 tier 2). Everything else
		// renders as a CUBE-xxxx block and exits 1, as before.
		code, render := cmd.ExitCodeFor(err)
		if render {
			// ui.RenderError: diag.Render verbatim in plain/JSON modes; a
			// styled panel on a rich terminal. Printed only after Execute
			// returned — i.e. after any live program fully released the
			// terminal — so the diagnosis is always the last thing shown
			// (design doc §5.2, diagnosis-last).
			fmt.Fprintln(os.Stderr, ui.RenderError(err))
		}
		stop() // os.Exit skips deferred calls; release the signal handler explicitly
		os.Exit(code)
	}
}
