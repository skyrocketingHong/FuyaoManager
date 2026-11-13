package caddymanage

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const AdminSocket = "/run/caddy/admin.sock"

type SystemRunner struct {
	UID  uint32
	GID  int
	Home string
}

func NewSystemRunner() (*SystemRunner, error) {
	account, err := user.Lookup("caddy")
	if err != nil {
		return nil, ErrUnavailable
	}
	uid, err := strconv.ParseUint(account.Uid, 10, 32)
	if err != nil {
		return nil, ErrUnavailable
	}
	gid, err := strconv.Atoi(account.Gid)
	if err != nil {
		return nil, ErrUnavailable
	}
	return &SystemRunner{UID: uint32(uid), GID: gid, Home: account.HomeDir}, nil
}

type configOutput struct{ bytes.Buffer }

func (b *configOutput) Write(data []byte) (int, error) {
	if b.Len()+len(data) > 4<<20 {
		return 0, ErrUnavailable
	}
	return b.Buffer.Write(data)
}

// Caddyfile {$ENV} adaptation must use the running daemon's environment.
// Secrets remain inside this root helper and its caddy-UID subprocesses;
// request JSON and result metadata never carry environment values.
func (r *SystemRunner) environment(ctx context.Context) ([]string, error) {
	command := exec.CommandContext(ctx, "/usr/bin/systemctl", "show", "caddy.service", "--property=MainPID", "--value")
	command.Env = []string{"PATH=/usr/bin:/bin", "LANG=C"}
	command.WaitDelay = 2 * time.Second
	data, err := command.Output()
	if err != nil {
		return nil, ErrUnavailable
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid < 1 {
		return nil, ErrUnavailable
	}
	file, err := os.Open("/proc/" + strconv.Itoa(pid) + "/environ")
	if err != nil {
		return nil, ErrUnavailable
	}
	defer file.Close()
	data, err = io.ReadAll(io.LimitReader(file, 1<<20))
	if err != nil || len(data) >= 1<<20 {
		return nil, ErrUnavailable
	}
	var env []string
	for _, entry := range bytes.Split(data, []byte{0}) {
		value := string(entry)
		key, _, ok := strings.Cut(value, "=")
		if !ok || oneOf(key, "LISTEN_FDS", "LISTEN_PID", "LISTEN_FDNAMES", "NOTIFY_SOCKET", "SYSTEMD_EXEC_PID", "PATH", "PWD") {
			continue
		}
		env = append(env, value)
	}
	return append(env, "PATH=/usr/bin:/bin", "PWD=/etc/caddy"), nil
}
func (r *SystemRunner) command(ctx context.Context, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	env, err := r.environment(ctx)
	if err != nil {
		return nil, err
	}
	command := exec.CommandContext(ctx, "/usr/bin/caddy", args...)
	command.Env = env
	command.Dir = "/etc/caddy"
	command.WaitDelay = 2 * time.Second
	command.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: r.UID, Gid: uint32(r.GID), Groups: []uint32{uint32(r.GID)}}}
	var output configOutput
	command.Stdout = &output
	if command.Run() != nil {
		return nil, ErrUnavailable
	}
	return output.Bytes(), nil
}
func (r *SystemRunner) Adapt(ctx context.Context, path string) ([]byte, error) {
	return r.command(ctx, "adapt", "--config", path, "--adapter", "caddyfile")
}
func (r *SystemRunner) Validate(ctx context.Context, path string) error {
	_, err := r.command(ctx, "validate", "--config", path, "--adapter", "caddyfile")
	return err
}
func (r *SystemRunner) Reload(ctx context.Context, path string) error {
	_, err := r.command(ctx, "reload", "--config", path, "--adapter", "caddyfile", "--address", "unix/"+AdminSocket)
	return err
}
func (r *SystemRunner) Active(ctx context.Context) ([]byte, error) {
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "unix", AdminSocket)
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 15 * time.Second}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1/config/", nil)
	if err != nil {
		return nil, ErrUnavailable
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return nil, ErrUnavailable
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 4<<20+1))
	if err != nil || len(data) > 4<<20 || !json.Valid(data) {
		return nil, ErrUnavailable
	}
	return data, nil
}
