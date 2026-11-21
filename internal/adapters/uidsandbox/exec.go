package uidsandbox

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"syscall"
	"time"
)

// Cmd is one command for the exec port. Argv is explicit: argv[0] is an absolute path and no element is ever joined
// into a shell string. The process gets only Env.
type Cmd struct {
	Argv   []string
	Env    []string
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	// Cancel, when set, runs once the context ends before the command does. The sandbox uses it to kill the uid's
	// processes through sudo, because the runner user cannot signal the root sudo process itself.
	Cancel func() error
}

// Exec runs commands. The exit status of a command that ran is returned with a nil error; the error is for a
// command that could not start or could not be waited for.
type Exec interface {
	Run(ctx context.Context, c Cmd) (int, error)
}

// OSExec runs commands with os/exec, each in its own process group (the localrunner pattern).
type OSExec struct{}

var _ Exec = OSExec{}

// waitDelay bounds how long Run waits for output pipes after a cancel.
const waitDelay = 5 * time.Second

// Run starts c and waits for it.
func (OSExec) Run(ctx context.Context, c Cmd) (int, error) {
	if len(c.Argv) == 0 {
		return -1, errors.New("exec: empty argv")
	}
	cmd := exec.CommandContext(ctx, c.Argv[0], c.Argv[1:]...) // #nosec G204 -- argv is explicit and built by this package, never a shell string
	cmd.Env = c.Env
	if cmd.Env == nil {
		cmd.Env = []string{}
	}
	cmd.Dir = "/"
	cmd.Stdin, cmd.Stdout, cmd.Stderr = c.Stdin, c.Stdout, c.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		var cerr error
		if c.Cancel != nil {
			cerr = c.Cancel()
		}
		if cmd.Process != nil && cmd.Process.Pid > 0 {
			// May be EPERM for a root process; the Cancel above is what kills the sandbox user.
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		return cerr
	}
	cmd.WaitDelay = waitDelay
	err := cmd.Run()
	if err == nil {
		return 0, nil
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode(), nil
	}
	return -1, fmt.Errorf("exec %s: %w", c.Argv[0], err)
}
