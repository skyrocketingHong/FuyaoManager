//go:build !unix

package buildrunner

import (
	"os/exec"
	"time"
)

// setupProcessGroup is the portable fallback: rely on the default context
// kill of the direct child plus WaitDelay.
func setupProcessGroup(cmd *exec.Cmd, waitDelay time.Duration) {
	cmd.WaitDelay = waitDelay
}
