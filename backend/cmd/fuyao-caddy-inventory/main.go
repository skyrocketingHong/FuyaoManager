package main

import (
	"fmt"
	"os"

	"github.com/skyrocketingHong/FuyaoManager/backend/internal/caddyinventory"
)

func main() {
	if len(os.Args) != 1 || caddyinventory.WriteSafe(os.Stdin, os.Stdout) != nil {
		_, _ = fmt.Fprintln(os.Stderr, "Caddy inventory unavailable")
		os.Exit(1)
	}
}
