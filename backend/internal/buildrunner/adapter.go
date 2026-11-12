// Adapter execution: the fixed, trusted command sequence for the
// node22-sveltekit-static adapter, run inside the task's private working
// copy with a scrubbed environment and its own process group.
package buildrunner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/skyrocketingHong/FuyaoManager/backend/internal/model"
)

// runCommand executes one step with a scrubbed environment: only PATH, HOME,
// TMPDIR, and the explicit FUYAO_* variables above. Database URLs, admin
// secrets, and anything else inherited from the management process are never
// forwarded to the theme's build. The command runs in its own process group
// and is killed group-wide on cancellation, so npm's children cannot survive
// and touch the shared workspace afterwards. This is environment and process
// hygiene, not an OS-level sandbox.
func (r *Runner) runCommand(ctx context.Context, build model.Build, stage, dir string, argv, extraEnv []string) (string, error) {
	name, args := argv[0], make([]string, 0, len(argv)-1)
	for _, arg := range argv[1:] {
		if len(arg) >= 6 && arg[:6] == "$THEME" {
			arg = filepath.Join(dir, arg[6:])
		}
		args = append(args, arg)
	}

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = append(scrubbedEnv(), extraEnv...)
	setupProcessGroup(cmd, 10*time.Second)
	logPath := filepath.Join(r.cfg.BuildsRoot, build.ID.String(), stage+".log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return "", err
	}
	defer logFile.Close()
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Run(); err != nil {
		return logPath, err
	}
	return logPath, nil
}

func scrubbedEnv() []string {
	var out []string
	// NODE_OPTIONS passes through for heap tuning on memory-constrained
	// hosts (e.g. --max-old-space-size); it cannot carry secrets or
	// arbitrary code into the build.
	for _, name := range []string{"PATH", "HOME", "TMPDIR", "LANG", "NODE_OPTIONS"} {
		if v, ok := os.LookupEnv(name); ok {
			out = append(out, name+"="+v)
		}
	}
	return out
}

func (r *Runner) writeInput(taskDir, name string, payload json.RawMessage) error {
	return os.WriteFile(filepath.Join(taskDir, name), append(payload, '\n'), 0o600)
}

func (r *Runner) commandFailure(stage, logPath string, err error) error {
	f := model.BuildFailure{Stage: stage, Message: "The " + stage + " step failed; see the task log."}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		code := exitErr.ExitCode()
		f.ExitCode = &code
		f.Message = fmt.Sprintf("The %s step exited with code %d; see the task log.", stage, code)
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		f.Message = "The " + stage + " step timed out or was cancelled."
	}
	_ = logPath
	return &buildError{failure: f}
}
