package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/config"
)

// resend (AM12g) sends one saved refused body again so a reader can bisect it offline: drop the tools, drop the system
// message, keep the last N messages, and see what the endpoint answers. It makes one request per call, never retries,
// and prints only the status, the content type and the first bytes of the answer. The key is read through
// internal/platform/config from -key-file and goes in the Authorization header only; it is never printed.

// resendBodyPrefix is how many bytes of the answer the command prints.
const resendBodyPrefix = 300

// resendTimeout bounds the one request.
const resendTimeout = 2 * time.Minute

// newResendClient makes the client of the one request. It never follows a redirect, because a redirect would send a
// second request with the key to wherever the endpoint points.
func newResendClient() *http.Client {
	return &http.Client{Timeout: resendTimeout, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
}

// resendOptions are the parts of the body the caller asks to leave out.
type resendOptions struct {
	dropTools  bool
	dropSystem bool
	keepLast   int
}

// resendFlags are the flags of the resend command.
type resendFlags struct {
	baseURL string
	model   string
	keyFile string
	body    string
	opts    resendOptions
}

func parseResendFlags(args []string, stderr io.Writer) (resendFlags, error) {
	fs := flag.NewFlagSet("resend", flag.ContinueOnError)
	fs.SetOutput(stderr)
	f := resendFlags{}
	fs.StringVar(&f.baseURL, "base-url", "https://openrouter.ai/api/v1", "the OpenAI-compatible API root")
	fs.StringVar(&f.model, "model", "", "the model id to send in the body (required)")
	fs.StringVar(&f.keyFile, "key-file", "", "the file that holds the model key, read through internal/platform/config (required)")
	fs.StringVar(&f.body, "body", "", "the saved request body to send (required)")
	fs.BoolVar(&f.opts.dropTools, "drop-tools", false, "send the body without its tools and tool_choice")
	fs.BoolVar(&f.opts.dropSystem, "drop-system", false, "send the body without its first system message")
	fs.IntVar(&f.opts.keepLast, "keep-last", 0, "send only the last N messages; 0 keeps them all")
	if err := fs.Parse(args); err != nil {
		return f, err
	}
	switch {
	case strings.TrimSpace(f.model) == "":
		return f, errors.New("resend: -model is required")
	case f.body == "":
		return f, errors.New("resend: -body is required")
	case f.keyFile == "":
		return f, errors.New("resend: -key-file is required")
	case f.opts.keepLast < 0:
		return f, fmt.Errorf("resend: -keep-last %d: want 0 or more", f.opts.keepLast)
	}
	return f, nil
}

// runResendCommand is the resend subcommand: it parses the flags and sends one body once.
func runResendCommand(ctx context.Context, args []string, stdout, stderr io.Writer) (int, error) {
	f, err := parseResendFlags(args, stderr)
	if err != nil {
		return exitUsage, err
	}
	return resendOnce(ctx, f.baseURL, f.model, f.keyFile, f.body, f.opts, stdout)
}

// resendOnce reads the saved body, reads the key and makes exactly one request. It is also the form a test calls.
func resendOnce(ctx context.Context, baseURL, model, keyFile, bodyPath string, opts resendOptions, stdout io.Writer) (int, error) {
	body, err := resendBody(bodyPath, model, opts)
	if err != nil {
		return exitUsage, err
	}
	key, err := resendKey(keyFile)
	if err != nil {
		return exitUsage, err
	}
	status, contentType, answer, err := resendSend(ctx, baseURL, body, key)
	if err != nil {
		return exitDisagree, err
	}
	writeDiagnostic(stdout, fmt.Sprintf("HTTP %d %s\n", status, contentType))
	writeDiagnostic(stdout, bounded(answer)+"\n")
	return exitAgrees, nil
}

// resendBody reads the saved body, sets the model the flag names, applies the parts to drop and returns the bytes to
// send. It reads at most the bound a saved body has.
func resendBody(path, model string, opts resendOptions) ([]byte, error) {
	raw, err := os.ReadFile(path) // #nosec G304 -- the operator names the saved body
	if err != nil {
		return nil, fmt.Errorf("resend: read the body: %w", err)
	}
	if len(raw) > maxRefusedBodyBytes {
		return nil, fmt.Errorf("resend: the body is larger than %d bytes", maxRefusedBodyBytes)
	}
	var wire any
	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil, fmt.Errorf("resend: the body is not JSON: %w", err)
	}
	obj, ok := wire.(map[string]any)
	if !ok {
		return nil, errors.New("resend: the body is not a JSON object")
	}
	if model != "" {
		obj["model"] = model
	}
	obj, err = transformResendBody(obj, opts)
	if err != nil {
		return nil, err
	}
	out, err := json.Marshal(obj)
	if err != nil {
		return nil, fmt.Errorf("resend: encode the body: %w", err)
	}
	return out, nil
}

// transformResendBody applies the parts to drop: the tools and their choice, the first system message, and every
// message but the last N. The rest of the body is kept as it was read.
func transformResendBody(body map[string]any, opts resendOptions) (map[string]any, error) {
	if opts.keepLast < 0 {
		return nil, fmt.Errorf("resend: -keep-last %d: want 0 or more", opts.keepLast)
	}
	if opts.dropTools {
		delete(body, "tools")
		delete(body, "tool_choice")
	}
	if msgs, ok := body["messages"].([]any); ok {
		msgs = dropSystemMessage(msgs, opts.dropSystem)
		if opts.keepLast > 0 && len(msgs) > opts.keepLast {
			msgs = msgs[len(msgs)-opts.keepLast:]
		}
		body["messages"] = msgs
	}
	return body, nil
}

// dropSystemMessage removes the first message when it is a system message.
func dropSystemMessage(msgs []any, drop bool) []any {
	if !drop || len(msgs) == 0 {
		return msgs
	}
	if m, ok := msgs[0].(map[string]any); ok && m["role"] == "system" {
		return msgs[1:]
	}
	return msgs
}

// resendKey reads the key through internal/platform/config from the file path, the same way the review command does.
// The key is returned to go in a header, never to be printed.
func resendKey(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resend: key file path: %w", err)
	}
	keys := keyFile{cfg: config.Config{SecretsDir: filepath.Dir(abs)}}
	key, err := keys.Get(filepath.Base(abs))
	if err != nil {
		return "", fmt.Errorf("resend: model key file: %w", err)
	}
	return key, nil
}

// resendSend makes exactly one request and returns the status, the content type and the answer body. It never retries,
// never follows a redirect (a redirect would be a second request) and never prints the key.
func resendSend(ctx context.Context, baseURL string, body []byte, key string) (status int, contentType string, answer []byte, err error) {
	ctx, cancel := context.WithTimeout(ctx, resendTimeout)
	defer cancel()
	url := strings.TrimRight(baseURL, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return 0, "", nil, fmt.Errorf("resend: build the request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := newResendClient().Do(req)
	if err != nil {
		return 0, "", nil, fmt.Errorf("resend: call the model endpoint: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	answer, err = io.ReadAll(io.LimitReader(resp.Body, maxRefusedBodyBytes))
	if err != nil {
		return resp.StatusCode, resp.Header.Get("Content-Type"), nil, fmt.Errorf("resend: read the answer: %w", err)
	}
	return resp.StatusCode, resp.Header.Get("Content-Type"), answer, nil
}

// bounded cuts the answer to the first resendBodyPrefix bytes for printing.
func bounded(answer []byte) string {
	if len(answer) <= resendBodyPrefix {
		return string(answer)
	}
	return string(answer[:resendBodyPrefix])
}
