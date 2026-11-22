package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
// call that failed after its retries, the bench writes each failed attempt to
// <refused dir>/<case>-turn<N>-<status>.json, so the body can be sent again with "heimdall-bench resend" and bisected
// offline. The product does not do this: it never writes a conversation to disk.

// maxRefusedBodies bounds the refused bodies the recorder holds. A case keeps at most one failed call with three
// attempts, and -parallel is at most maxParallel, so this holds every body the run can still save.
const maxRefusedBodies = 4 * maxParallel

// refusedHashPrefix is how many hex characters of the SHA-256 name a saved body.
const refusedHashPrefix = 12

// maxRefusedBodyBytes bounds a saved body. Live run 5 refused bodies of 3 KB to 211 KB; a body past this bound is
// refused rather than written, and the index says nothing about it.
const maxRefusedBodyBytes = 1 << 20

// savedRefusedDirName is the folder under the OS temp directory when -refused-dir is not set.
const savedRefusedDirName = "heimdall-bench-refused"

// refusedIndexName is the index file in the refused folder. It lists each saved body so a reader can pick one to send.
const refusedIndexName = "index.json"

// refusedIndexNote says what a saved file holds, so a reader does not look for a header or a key in it.
const refusedIndexNote = "The request bodies the model endpoint refused, one file each. A file holds the JSON request " +
	"body only: no header and no key. Send one again with: heimdall-bench resend -body <file> -model <id> " +
	"-key-file <path>."

// defaultRefusedDir is the -refused-dir default, a folder under the OS temp directory.
func defaultRefusedDir() string { return filepath.Join(os.TempDir(), savedRefusedDirName) }

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
	if err != nil || resp == nil || resp.StatusCode < 200 || resp.StatusCode >= 300 {
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

func (c liveChat) refusedBody(sha256Hex string) ([]byte, bool) {
	return c.bodies.refusedBody(sha256Hex)
}

// refusedEntry is one saved refusal in the index. It names the case, the turn of the call, the HTTP status and the
// body's SHA-256, never the body's content. Attempts is how many attempts of the call sent this same body.
type refusedEntry struct {
	Case     string `json:"case"`
	Turn     int    `json:"turn"`
	Status   int    `json:"status"`
	Kind     string `json:"kind,omitempty"`
	Bytes    int    `json:"bytes"`
	SHA256   string `json:"sha256"`
	Attempts int    `json:"attempts"`
	File     string `json:"file"`
}

// refusedIndex is the index file of the refused folder.
type refusedIndex struct {
	Note     string         `json:"note"`
	Refusals []refusedEntry `json:"refusals"`
}

// refusedStore writes the refused bodies of one run and keeps its index. One store is shared by every worker of a
// parallel run, so it locks itself. An empty dir saves nothing. A folder that already holds an index keeps the entries
// of the runs before it, so the index always names every file the folder holds.
type refusedStore struct {
	dir     string
	mu      sync.Mutex
	entries []refusedEntry
}

func newRefusedStore(dir string) *refusedStore {
	return &refusedStore{dir: dir, entries: loadRefusedIndex(dir)}
}

// loadRefusedIndex reads the index a run before this one saved, so its entries are kept. A missing or unreadable index
// starts empty: the next save writes a fresh one.
func loadRefusedIndex(dir string) []refusedEntry {
	if dir == "" {
		return nil
	}
	raw, err := os.ReadFile(filepath.Join(dir, refusedIndexName)) // #nosec G304 -- the folder is the operator's
	if err != nil {
		return nil
	}
	var index refusedIndex
	if err := json.Unmarshal(raw, &index); err != nil {
		return nil
	}
	return index.Refusals
}

// save writes the body of each failed attempt to <dir>/<case>-turn<N>-<status>.json and returns the path of each
// attempt in order, "" for an attempt whose body the source does not hold or one past the size bound. A body saved
// before in the same case and turn is not written again; its attempt count grows.
func (s *refusedStore) save(caseID string, turn int, attempts []ports.AgentAttempt, src refusedSource) ([]string, error) {
	paths := make([]string, len(attempts))
	if s == nil || s.dir == "" || src == nil {
		return paths, nil
	}
	var errs []error
	for i, a := range attempts {
		p, err := s.saveOne(caseID, turn, a, src)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		paths[i] = p
	}
	return paths, errors.Join(errs...)
}

// saveOne writes one attempt's body and records it. It returns "" when the body is not held or is past the bound, so
// a run with no refusal writes no folder at all.
func (s *refusedStore) saveOne(caseID string, turn int, a ports.AgentAttempt, src refusedSource) (string, error) {
	if a.Status == 0 || len(a.RequestHash) < refusedHashPrefix {
		return "", nil
	}
	body, ok := src.refusedBody(a.RequestHash)
	if !ok || len(body) > maxRefusedBodyBytes {
		return "", nil
	}
	if err := os.MkdirAll(s.dir, 0o750); err != nil {
		return "", fmt.Errorf("save the refused request: %w", err)
	}
	path := filepath.Join(s.dir, refusedFileName(caseID, turn, a.Status))
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.find(path)
	switch {
	case i >= 0 && s.entries[i].SHA256 == a.RequestHash:
		// A retry of the same body on the same call: one file, one more attempt.
		s.entries[i].Attempts++
		return path, s.writeIndexLocked()
	case i >= 0:
		// Another body on the same call and status (a tool_choice fallback): keep both files.
		path = filepath.Join(s.dir, refusedFileNameHash(caseID, turn, a.Status, a.RequestHash))
		if j := s.find(path); j >= 0 {
			s.entries[j].Attempts++
			return path, s.writeIndexLocked()
		}
	}
	if err := writeRefused(path, body); err != nil {
		return "", err
	}
	s.entries = append(s.entries, refusedEntry{Case: safeName(caseID), Turn: turn, Status: a.Status,
		Kind: string(a.Kind), Bytes: len(body), SHA256: a.RequestHash, Attempts: 1, File: filepath.Base(path)})
	return path, s.writeIndexLocked()
}

// find returns the index of the saved entry at path, or -1.
func (s *refusedStore) find(path string) int {
	name := filepath.Base(path)
	for i, e := range s.entries {
		if e.File == name {
			return i
		}
	}
	return -1
}

// writeIndexLocked rewrites the index file from the entries. The caller holds the lock.
func (s *refusedStore) writeIndexLocked() error {
	b, err := json.MarshalIndent(refusedIndex{Note: refusedIndexNote, Refusals: s.entries}, "", "  ")
	if err != nil {
		return fmt.Errorf("save the refused request index: %w", err)
	}
	path := filepath.Join(s.dir, refusedIndexName)
	if err := os.WriteFile(path, append(b, '\n'), 0o600); err != nil {
		return fmt.Errorf("save the refused request index: %w", err)
	}
	return nil
}

// refusedFileName names one saved body: the case, the turn of the call and the HTTP status.
func refusedFileName(caseID string, turn, status int) string {
	return fmt.Sprintf("%s-turn%d-%d.json", safeName(caseID), turn, status)
}

// refusedFileNameHash names a second body on the same case, turn and status, so two bodies never share one file.
func refusedFileNameHash(caseID string, turn, status int, hash string) string {
	return fmt.Sprintf("%s-turn%d-%d-%s.json", safeName(caseID), turn, status, hash[:refusedHashPrefix])
}

func writeRefused(path string, body []byte) error {
	if err := os.WriteFile(path, body, 0o600); err != nil {
		return fmt.Errorf("save the refused request: %w", err)
	}
	return nil
}

// safeName keeps letters, digits, dash and underscore of a case id, so the id cannot name another directory. Any other
// character becomes one dash, and the leading and trailing dashes go.
func safeName(id string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_':
			b.WriteRune(r)
			lastDash = false
		case b.Len() > 0 && !lastDash:
			b.WriteByte('-')
			lastDash = true
		}
	}
	name := strings.Trim(b.String(), "-")
	if name == "" {
		return "case"
	}
	return name
}
