// Command cube-idp is the cube-idp binary entrypoint.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/cube-idp/cube-idp/internal/cli"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	os.Exit(cli.Execute(ctx, os.Args[1:], os.Stdout, os.Stderr))
}
