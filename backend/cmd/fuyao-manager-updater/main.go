// fuyao-manager-updater runs independently of the application it replaces.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"os/user"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/skyrocketingHong/FuyaoManager/backend/internal/buildinfo"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/platformupdate"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func trusted(path string) error {
	for p := filepath.Clean(path); ; p = filepath.Dir(p) {
		info, err := os.Lstat(p)
		if err != nil {
			return err
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Uid != 0 || info.Mode().Perm()&0022 != 0 || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("untrusted updater path: %s", p)
		}
		if p == filepath.Dir(p) {
			break
		}
	}
	return nil
}

func run() error {
	if len(os.Args) == 2 && os.Args[1] == "version" {
		return json.NewEncoder(os.Stdout).Encode(buildinfo.Current())
	}
	if len(os.Args) == 10 && os.Args[1] == "stage" && os.Geteuid() == 0 {
		runID, err := strconv.ParseInt(os.Args[6], 10, 64)
		if err != nil {
			return err
		}
		attempt, err := strconv.Atoi(os.Args[7])
		if err != nil {
			return err
		}
		if os.Args[9] != "--verified-download" {
			return fmt.Errorf("verify the GitHub download digest before staging")
		}
		manifest, err := platformupdate.Stage(os.Args[2], os.Args[3], os.Args[4], platformupdate.Version{Commit: os.Args[5], RunID: runID, Attempt: attempt, Branch: os.Args[8]})
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(manifest)
	}
	if len(os.Args) != 2 || os.Geteuid() != 0 {
		return fmt.Errorf("usage: fuyao-manager-updater /root-owned/config.json (root required)")
	}
	if err := trusted(os.Args[1]); err != nil {
		return err
	}
	raw, err := os.ReadFile(os.Args[1])
	if err != nil {
		return err
	}
	var cfg platformupdate.Config
	if err = json.Unmarshal(raw, &cfg); err != nil {
		return err
	}
	if err = cfg.Validate(); err != nil {
		return err
	}
	for _, p := range []string{cfg.Root, filepath.Join(cfg.Root, "releases"), cfg.PrepareCommand} {
		if err = trusted(p); err != nil {
			return err
		}
	}
	lock, err := os.OpenFile(filepath.Join(cfg.Root, ".updater.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return fmt.Errorf("another updater owns this installation")
	}
	m, err := platformupdate.New(cfg)
	if err != nil {
		return err
	}
	group, err := user.LookupGroup(cfg.Group)
	if err != nil {
		return err
	}
	gid, err := strconv.Atoi(group.Gid)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(cfg.Socket), 0750); err != nil {
		return err
	}
	if err = os.Chown(filepath.Dir(cfg.Socket), 0, gid); err != nil {
		return err
	}
	if err = os.Remove(cfg.Socket); err != nil && !os.IsNotExist(err) {
		return err
	}
	listener, err := net.Listen("unix", cfg.Socket)
	if err != nil {
		return err
	}
	defer listener.Close()
	if err = os.Chown(cfg.Socket, 0, gid); err != nil {
		return err
	}
	if err = os.Chmod(cfg.Socket, 0660); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	server := &http.Server{Handler: m.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 45 * time.Second, IdleTimeout: 60 * time.Second}
	m.SetRestart(func() { stop() })
	go m.Loop(ctx)
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	err = server.Serve(listener)
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}
