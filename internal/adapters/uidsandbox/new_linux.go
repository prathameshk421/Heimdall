//go:build linux

package uidsandbox

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// toolDirs are the only places binaries are looked up. The job's PATH is never used for them.
func toolDirs() []string { return []string{"/usr/sbin", "/usr/bin", "/sbin", "/bin"} }

func findTool(name string) (string, error) {
	for _, d := range toolDirs() {
		p := filepath.Join(d, name)
		if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() && fi.Mode().Perm()&0o111 != 0 {
			return p, nil
		}
	}
	return "", &CannotIsolateError{Reason: "missing tool " + name}
}

func lookupTools() (tools, error) {
	var t tools
	targets := map[string]*string{
		"sudo": &t.sudo, "useradd": &t.useradd, "id": &t.id, "nft": &t.nft,
		"systemd-run": &t.systemdRun, "systemctl": &t.systemctl, "env": &t.env,
		"tar": &t.tar, "pkill": &t.pkill, "pgrep": &t.pgrep, "chmod": &t.chmod, "chown": &t.chown,
		"mkdir": &t.mkdir, "rm": &t.rm, "tee": &t.tee, "head": &t.head, "sh": &t.sh,
	}
	var errs []error
	for name, dst := range targets {
		p, err := findTool(name)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		*dst = p
	}
	if len(errs) > 0 {
		return tools{}, errors.Join(errs...)
	}
	// crontab is optional: without it there is no cron to deny.
	if p, err := findTool("crontab"); err == nil {
		t.crontab = p
	}
	return t, nil
}

// New checks that this runner can isolate PR code and returns the sandbox. It returns a *CannotIsolateError when
// sudo needs a password or is absent, when systemd is not PID 1, when nftables is unusable or when a tool is
// missing. It changes nothing on the host; Setup does that.
func New(ctx context.Context, cfg Config, ex Exec) (*Sandbox, error) {
	t, err := lookupTools()
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat("/run/systemd/system"); errors.Is(err, fs.ErrNotExist) {
		return nil, &CannotIsolateError{Reason: "systemd is not the init system"}
	}
	s := newSandbox(cfg, ex, t)
	code, out, err := s.root(ctx, nil, t.env, "true")
	if err != nil {
		return nil, fmt.Errorf("privilege check: %w", err)
	}
	if code != 0 {
		return nil, &CannotIsolateError{Reason: "passwordless root is unavailable: " + out}
	}
	code, out, err = s.root(ctx, nil, t.nft, "list", "tables")
	if err != nil {
		return nil, fmt.Errorf("nft check: %w", err)
	}
	if code != 0 {
		return nil, &CannotIsolateError{Reason: "nftables unusable: " + out}
	}
	return s, nil
}
