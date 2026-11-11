//go:build unix

package buildrunner

import (
	"os/exec"
	"syscall"
	"time"
)

// setupProcessGroup puts the command in its own process group and arranges a
// group-wide kill on context cancellation plus a bounded WaitDelay, so a
// cancelled npm cannot leave vite/node children writing into the shared
// workspace after the runner reports the step dead.
func setupProcessGroup(cmd *exec.Cmd, waitDelay time.Duration) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if cmd.Process != nil { // Process is nil before Start; Cancel runs after
		return
	}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = waitDelay
}
