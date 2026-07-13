package main

import (
	"fmt"
	"os"

	"github.com/rafpe/cube-idp/cmd"
	"github.com/rafpe/cube-idp/internal/diag"
)

func main() {
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, diag.Render(err))
		os.Exit(1)
	}
}
