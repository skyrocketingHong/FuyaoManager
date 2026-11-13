package main

import (
	"context"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/caddymanage"
	"os"
	"time"
)

func main() {
	if os.Geteuid() != 0 || len(os.Args) != 1 {
		_ = caddymanage.WriteResponse(os.Stdout, caddymanage.Result{}, caddymanage.ErrPermission)
		os.Exit(1)
	}
	request, err := caddymanage.DecodeRequest(os.Stdin)
	if err != nil {
		_ = caddymanage.WriteResponse(os.Stdout, caddymanage.Result{}, err)
		os.Exit(1)
	}
	runner, err := caddymanage.NewSystemRunner()
	if err != nil {
		_ = caddymanage.WriteResponse(os.Stdout, caddymanage.Result{}, err)
		os.Exit(1)
	}
	engine := caddymanage.NewEngine(caddymanage.RegistryRoot, caddymanage.StateRoot, runner, runner.GID)
	ctx, cancel := context.WithTimeout(context.Background(), 110*time.Second)
	defer cancel()
	result, err := engine.Run(ctx, request)
	if caddymanage.WriteResponse(os.Stdout, result, err) != nil || err != nil {
		os.Exit(1)
	}
}
