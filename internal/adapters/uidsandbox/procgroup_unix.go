//go:build !windows

package uidsandbox

import (
	"os/exec"
	"syscall"
)

// ownProcessGroup starts the command in a new process group, so a cancel reaches every child it started.
func ownProcessGroup(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} }

// killProcessGroup kills the command's whole process group. The error is ignored: it may be EPERM for a root process.
func killProcessGroup(cmd *exec.Cmd) { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
