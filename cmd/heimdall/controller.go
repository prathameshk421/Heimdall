package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/url"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var controllerNamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,62}$`)

type controllerDeps struct {
	prompt  hiddenSecretPrompt
	enroll  func(context.Context, string, string, string, string) error
	timeout time.Duration
}

func realControllerDeps() controllerDeps {
	return controllerDeps{
		prompt:  readHiddenSecret,
		timeout: 2 * time.Minute,
		enroll: func(ctx context.Context, server, name, dataDir, code string) error {
			// The upstream CLI accepts its one-time enrollment code only as an argument. Capture and
			// discard all upstream output so an error cannot echo the code into the operator's terminal.
			// #nosec G204 -- fixed executable, no shell; server/name/path are strictly validated and the one-time code is the CLI's required credential input.
			cmd := exec.CommandContext(ctx, "switch-agent-controller", "enroll", "--server", server,
				"--code", code, "--name", name, "--data-dir", dataDir, "--secret-store", "file")
			if err := cmd.Run(); err != nil {
				return errors.New("upstream enrollment command failed")
			}
			return nil
		},
	}
}

type controllerEnrollOptions struct {
	server  string
	name    string
	dataDir string
}

func runController(args []string, _ io.Reader, stdout, stderr io.Writer, deps controllerDeps) int {
	opts, ok := parseControllerEnroll(args, stderr)
	if !ok {
		return 2
	}
	return enrollController(opts, stdout, stderr, deps)
}

func parseControllerEnroll(args []string, stderr io.Writer) (controllerEnrollOptions, bool) {
	if len(args) == 0 || args[0] != "enroll" {
		writeDiagnostic(stderr, "usage: heimdall controller enroll --server URL --name NAME --data-dir PATH\n")
		return controllerEnrollOptions{}, false
	}
	fs := flag.NewFlagSet("heimdall controller enroll", flag.ContinueOnError)
	fs.SetOutput(stderr)
	server := fs.String("server", "", "Switch server URL")
	name := fs.String("name", "", "controller machine name")
	dataDir := fs.String("data-dir", "", "persistent controller state directory")
	if err := fs.Parse(args[1:]); err != nil {
		return controllerEnrollOptions{}, false
	}
	if fs.NArg() != 0 || !validControllerServer(*server) || !controllerNamePattern.MatchString(*name) || !filepath.IsAbs(*dataDir) || filepath.Clean(*dataDir) != *dataDir || strings.ContainsAny(*dataDir, "\r\n\x00") {
		writeDiagnostic(stderr, "heimdall controller enroll: provide a valid server URL, machine name, and clean absolute data directory\n")
		return controllerEnrollOptions{}, false
	}
	return controllerEnrollOptions{server: *server, name: *name, dataDir: *dataDir}, true
}

func enrollController(opts controllerEnrollOptions, stdout, stderr io.Writer, deps controllerDeps) int {
	if deps.prompt == nil || deps.enroll == nil {
		writeDiagnostic(stderr, "heimdall controller enroll: enrollment is unavailable\n")
		return 1
	}
	code, err := deps.prompt(stderr)
	if err != nil {
		clear(code)
		writeDiagnostic(stderr, "heimdall controller enroll: hidden one-time code input failed\n")
		return 1
	}
	defer clear(code)
	if len(code) == 0 || len(code) > 512 || strings.ContainsAny(string(code), "\r\n\x00") {
		writeDiagnostic(stderr, "heimdall controller enroll: one-time code is empty or malformed\n")
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), deps.timeout)
	defer cancel()
	if err := deps.enroll(ctx, opts.server, opts.name, opts.dataDir, string(code)); err != nil {
		writeDiagnostic(stderr, "heimdall controller enroll: Switch rejected enrollment or the controller is unreachable; issue a fresh one-time code and retry\n")
		return 1
	}
	if _, err := fmt.Fprintf(stdout, "controller %s enrolled; persistent state is at %s\nNext: run deploy/agents/install-controller.sh --controller-data-dir %s with the same controller user\n", opts.name, opts.dataDir, opts.dataDir); err != nil {
		writeDiagnostic(stderr, "heimdall controller enroll: output failed\n")
		return 1
	}
	return 0
}

func validControllerServer(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		return true
	case "http":
		host := strings.ToLower(u.Hostname())
		if host == "localhost" {
			return true
		}
		ip := net.ParseIP(host)
		return ip != nil && ip.IsLoopback()
	default:
		return false
	}
}
