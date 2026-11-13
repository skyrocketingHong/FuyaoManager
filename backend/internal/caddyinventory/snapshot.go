// Package caddyinventory runs only the operator-selected read capability.
package caddyinventory

import (
	"bytes"
	"context"
	"fmt"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/caddyconfig"
	"os/exec"
	"time"
)

type bounded struct{ bytes.Buffer }

func (b *bounded) Write(p []byte) (int, error) {
	if b.Len()+len(p) > caddyconfig.MaxConfigBytes {
		return 0, caddyconfig.ErrTooLarge
	}
	return b.Buffer.Write(p)
}
func Read(ctx context.Context, argv []string) ([]caddyconfig.Site, error) {
	if len(argv) == 0 {
		return nil, fmt.Errorf("Caddy read capability not configured")
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Env = []string{"PATH=/usr/bin:/bin", "LANG=C"}
	cmd.WaitDelay = 2 * time.Second
	var data bounded
	cmd.Stdout = &data
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("Caddy read capability failed")
	}
	return caddyconfig.Parse(data.Bytes())
}
