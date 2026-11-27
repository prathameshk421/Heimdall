//go:build windows

package uidsandbox

import "os/exec"

// The proof sandbox runs on Linux only (New refuses other systems). On Windows the CLI still builds, and a command run
// through OSExec is killed by itself, because Windows has no Unix process groups.
func ownProcessGroup(*exec.Cmd) {}

// killProcessGroup kills the command. The error is ignored, as on Unix: the process may already be gone.
func killProcessGroup(cmd *exec.Cmd) { _ = cmd.Process.Kill() }
