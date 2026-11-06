package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestControllerEnrollUsesHiddenOneTimeCodeAndPrintsOnlyHandoff(t *testing.T) {
	var out, errOut bytes.Buffer
	code := []byte("test-only-enrollment-code")
	promptCalls := 0
	var gotServer, gotName, gotDir, gotCode string
	deps := controllerDeps{
		prompt: func(io.Writer) ([]byte, error) {
			promptCalls++
			return append([]byte(nil), code...), nil
		},
		timeout: time.Second,
		enroll: func(_ context.Context, server, name, dataDir, enrollmentCode string) error {
			gotServer, gotName, gotDir, gotCode = server, name, dataDir, enrollmentCode
			return nil
		},
	}
	result := runController([]string{"enroll", "--server", "https://switch.example.org", "--name", "heimdall-vm", "--data-dir", "/var/lib/switch-controller"}, nil, &out, &errOut, deps)
	require.Zero(t, result, errOut.String())
	require.Equal(t, 1, promptCalls)
	require.Equal(t, "https://switch.example.org", gotServer)
	require.Equal(t, "heimdall-vm", gotName)
	require.Equal(t, "/var/lib/switch-controller", gotDir)
	require.Equal(t, "test-only-enrollment-code", gotCode)
	require.NotContains(t, out.String()+errOut.String(), gotCode)
	require.Contains(t, out.String(), "controller heimdall-vm enrolled")
	require.Contains(t, out.String(), "install-controller.sh")
}

func TestControllerEnrollRefusesUnsafeArgumentsBeforePrompt(t *testing.T) {
	for _, args := range [][]string{
		{"enroll", "--server", "https://switch.example.org/path", "--name", "node", "--data-dir", "/var/lib/node"},
		{"enroll", "--server", "https://user:password@switch.example.org", "--name", "node", "--data-dir", "/var/lib/node"},
		{"enroll", "--server", "http://switch.example.org", "--name", "node", "--data-dir", "/var/lib/node"},
		{"enroll", "--server", "https://switch.example.org", "--name", "bad name", "--data-dir", "/var/lib/node"},
		{"enroll", "--server", "https://switch.example.org", "--name", "node", "--data-dir", "relative/path"},
	} {
		t.Run(args[2], func(t *testing.T) {
			prompted := false
			var out, errOut bytes.Buffer
			deps := controllerDeps{
				prompt:  func(io.Writer) ([]byte, error) { prompted = true; return []byte("test-only-enrollment-code"), nil },
				enroll:  func(context.Context, string, string, string, string) error { return nil },
				timeout: time.Second,
			}
			require.Equal(t, 2, runController(args, nil, &out, &errOut, deps))
			require.False(t, prompted)
			require.Empty(t, out.String())
		})
	}
}

func TestControllerEnrollFailureDoesNotEchoUpstreamOutput(t *testing.T) {
	const code = "test-only-enrollment-code"
	var out, errOut bytes.Buffer
	deps := controllerDeps{
		prompt:  func(io.Writer) ([]byte, error) { return []byte(code), nil },
		timeout: time.Second,
		enroll: func(context.Context, string, string, string, string) error {
			return errors.New("upstream error included " + code)
		},
	}
	require.Equal(t, 1, runController([]string{"enroll", "--server", "http://127.0.0.1:8000", "--name", "local", "--data-dir", "/tmp/controller"}, nil, &out, &errOut, deps))
	require.NotContains(t, out.String()+errOut.String(), code)
	require.Contains(t, errOut.String(), "issue a fresh one-time code")
}

func TestControllerEnrollSecretPromptFailureIsGeneric(t *testing.T) {
	const code = "test-only-enrollment-code"
	var out, errOut bytes.Buffer
	deps := controllerDeps{
		prompt:  func(io.Writer) ([]byte, error) { return []byte(code), errors.New(code) },
		timeout: time.Second,
		enroll:  func(context.Context, string, string, string, string) error { t.Fatal("must not enroll"); return nil },
	}
	require.Equal(t, 1, runController([]string{"enroll", "--server", "https://switch.example.org", "--name", "node", "--data-dir", "/var/lib/node"}, nil, &out, &errOut, deps))
	require.NotContains(t, out.String()+errOut.String(), code)
}
