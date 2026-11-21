//go:build !linux

package uidsandbox

import "context"

// New always fails off Linux: the sandbox needs Linux users, nftables and systemd.
func New(_ context.Context, _ Config, _ Exec) (*Sandbox, error) {
	return nil, &CannotIsolateError{Reason: "not a Linux runner"}
}
