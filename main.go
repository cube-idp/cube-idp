package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/rafpe/cube-idp/cmd"
	"github.com/rafpe/cube-idp/internal/diag"
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
			fmt.Fprintln(os.Stderr, diag.Render(err))
		}
		stop() // os.Exit skips deferred calls; release the signal handler explicitly
		os.Exit(code)
	}
}
