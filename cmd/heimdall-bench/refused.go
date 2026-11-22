package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/openrouter"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// Refused request bodies (AM12g). Deploy refuses some request bodies with HTTP 500 every time (live run 5: seven
// cases, each one body refused three times, one of them the case's first request). The bench keeps the body of each
// refused request in memory by its SHA-256, the same hash the chat client puts in its error. When a case ends on a
// call that failed after its retries, the bench writes the body of each failed attempt to
// <out dir>/refused/<case>-<sha256 prefix>.json, so the body can be sent again with "heimdall-bench resend" and
// bisected offline. The product does not do this: it never writes a conversation to disk.

// maxRefusedBodies bounds the refused bodies the recorder holds. A case keeps at most one failed call with three
// attempts, and -parallel is at most maxParallel, so this holds every body the run can still save.
const maxRefusedBodies = 4 * maxParallel

// refusedHashPrefix is how many hex characters of the SHA-256 name a saved body.
const refusedHashPrefix = 12

// refusedSource gives the body of a refused request by the SHA-256 the chat client recorded for it.
type refusedSource interface {
	refusedBody(sha256Hex string) ([]byte, bool)
}

// bodyRecorder is the HTTP transport of the bench's chat client. It sends each request through inner and keeps the
// body of each request the endpoint did not answer with 2xx. It never keeps a header, and it refuses to keep a body
// that holds the bearer key of its own request, so a saved body cannot leak the key.
type bodyRecorder struct {
	inner http.RoundTripper
	mu    sync.Mutex
	order []string
	byKey map[string][]byte
}

func newBodyRecorder(inner http.RoundTripper) *bodyRecorder {
	return &bodyRecorder{inner: inner, byKey: map[string][]byte{}}
}

// RoundTrip implements http.RoundTripper. It reads the body before the request goes out, because the transport
// consumes it.
func (b *bodyRecorder) RoundTrip(req *http.Request) (*http.Response, error) {
	var body []byte
	if req.Body != nil && req.GetBody != nil {
		rc, err := req.GetBody()
		if err != nil {
			return nil, fmt.Errorf("copy the model request body: %w", err)
		}
		body, err = io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			return nil, fmt.Errorf("copy the model request body: %w", err)
		}
	}
	resp, err := b.inner.RoundTrip(req)
	if err != nil || resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b.keep(body, bearerOf(req.Header))
	}
	return resp, err
}

// bearerOf returns the key of an Authorization: Bearer header, or "" when there is none.
func bearerOf(h http.Header) string {
	v, ok := strings.CutPrefix(h.Get("Authorization"), "Bearer ")
	if !ok {
		return ""
	}
	return strings.TrimSpace(v)
}

// keep holds body by its SHA-256, the oldest dropped past maxRefusedBodies. A body that contains key is not kept.
func (b *bodyRecorder) keep(body []byte, key string) {
	if len(body) == 0 || (key != "" && bytes.Contains(body, []byte(key))) {
		return
	}
	sum := sha256.Sum256(body)
	id := hex.EncodeToString(sum[:])
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.byKey[id]; ok {
		return
	}
	b.byKey[id] = bytes.Clone(body)
	b.order = append(b.order, id)
	if len(b.order) > maxRefusedBodies {
		delete(b.byKey, b.order[0])
		b.order = b.order[1:]
	}
}

func (b *bodyRecorder) refusedBody(sha256Hex string) ([]byte, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	body, ok := b.byKey[sha256Hex]
	return body, ok
}

// liveChat is the bench's chat client with its body recorder.
type liveChat struct {
	openrouter.ChatClient
	bodies *bodyRecorder
}

func (c liveChat) refusedBody(sha256Hex string) ([]byte, bool) { return c.bodies.refusedBody(sha256Hex) }

// saveRefused writes the body of each failed attempt to dir/refused/<case>-<sha256 prefix>.json, once per body, and
// returns the saved path of each attempt in order ("" for an attempt whose body the source does not hold). It writes
// nothing when the run did not end on a failed call.
func saveRefused(dir, caseID string, attempts []ports.AgentAttempt, src refusedSource) ([]string, error) {
	if len(attempts) == 0 || src == nil {
		return nil, nil
	}
	paths := make([]string, len(attempts))
	written := map[string]string{}
	var errs []error
	for i, a := range attempts {
		if len(a.RequestHash) < refusedHashPrefix {
			continue
		}
		if p, ok := written[a.RequestHash]; ok {
			paths[i] = p
			continue
		}
		body, ok := src.refusedBody(a.RequestHash)
		if !ok {
			continue
		}
		p := filepath.Join(dir, "refused", safeName(caseID)+"-"+a.RequestHash[:refusedHashPrefix]+".json")
		if err := writeRefused(p, body); err != nil {
			errs = append(errs, err)
			continue
		}
		paths[i], written[a.RequestHash] = p, p
	}
	return paths, errors.Join(errs...)
}

func writeRefused(path string, body []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("save the refused request: %w", err)
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		return fmt.Errorf("save the refused request: %w", err)
	}
	return nil
}

// safeName keeps letters, digits, dash and underscore of a case id, so the id cannot name another directory.
func safeName(id string) string {
	name := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		}
		return '-'
	}, id)
	if name == "" {
		return "case"
	}
	return name
}
