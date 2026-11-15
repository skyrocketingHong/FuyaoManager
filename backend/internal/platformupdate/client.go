package platformupdate

import (
	"context"
	"net"
	"net/http"
	"time"
)

// Client uses a fixed operator-configured Unix socket. Browser input cannot
// select an upstream URL, local file, command or systemd service.
func Client(socket string) *http.Client {
	return &http.Client{Timeout: 40 * time.Second, Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, "unix", socket)
		},
	}}
}
