package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/rafpe/cube-idp/cmd"
	"github.com/rafpe/cube-idp/internal/diag"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if err := cmd.Execute(ctx); err != nil {
		fmt.Fprintln(os.Stderr, diag.Render(err))
		os.Exit(1)
	}
}
