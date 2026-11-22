package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

const testKey = "sk-test-not-a-real-key"

// mapSource is a refusedSource with a fixed set of bodies.
type mapSource map[string][]byte

func (m mapSource) refusedBody(sha256Hex string) ([]byte, bool) {
	b, ok := m[sha256Hex]
	return b, ok
}

func hashOf(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func refusedServer(t *testing.T, status int, answer string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = io.WriteString(w, answer)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// refusedBodyThrough sends body to srv through a recorder, the way the chat client does, and returns the recorder and
// the body hash.
func refusedBodyThrough(t *testing.T, srv *httptest.Server, body []byte, key string) (*bodyRecorder, string) {
	t.Helper()
	rec := newBodyRecorder(http.DefaultTransport)
	client := &http.Client{Transport: rec}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, srv.URL, bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := client.Do(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	return rec, hashOf(body)
}

func TestBodyRecorderKeepsTheRefusedBodyAndNeverAHeader(t *testing.T) {
	body := []byte(`{"model":"m","messages":[{"role":"user","content":"review this change"}]}`)
	srv := refusedServer(t, http.StatusInternalServerError, `{"error":"boom"}`)
	rec, hash := refusedBodyThrough(t, srv, body, testKey)

	saved, ok := rec.refusedBody(hash)
	require.True(t, ok, "a 500 keeps the body")
	require.Equal(t, body, saved)
	require.NotContains(t, string(saved), testKey, "the recorder keeps no header, so no key")
	require.NotContains(t, string(saved), "Authorization")
	require.NotContains(t, string(saved), "Content-Type")
}

func TestBodyRecorderKeepsNothingForAnAnsweredCall(t *testing.T) {
	body := []byte(`{"model":"m","messages":[]}`)
	srv := refusedServer(t, http.StatusOK, `{"id":"x"}`)
	rec, hash := refusedBodyThrough(t, srv, body, testKey)

	_, ok := rec.refusedBody(hash)
	require.False(t, ok, "an answered call is not a refusal")
}

func TestBodyRecorderRefusesABodyThatHoldsTheKey(t *testing.T) {
	// A body that carries the key itself (a bug in a caller) must never reach a file.
	body := []byte(`{"model":"m","messages":[{"role":"user","content":"` + testKey + `"}]}`)
	srv := refusedServer(t, http.StatusBadRequest, `{"error":"bad"}`)
	rec, hash := refusedBodyThrough(t, srv, body, testKey)

	_, ok := rec.refusedBody(hash)
	require.False(t, ok, "a body that holds the key is never kept")
}

func TestBodyRecorderDropsTheOldestBodyPastItsBound(t *testing.T) {
	rec := newBodyRecorder(http.DefaultTransport)
	for i := range maxRefusedBodies + 3 {
		rec.keep([]byte(strings.Repeat("b", i+1)), "")
	}
	rec.mu.Lock()
	held := len(rec.byKey)
	rec.mu.Unlock()
	require.Equal(t, maxRefusedBodies, held, "the recorder bounds what it holds")
	_, ok := rec.refusedBody(hashOf([]byte(strings.Repeat("b", 1))))
	require.False(t, ok, "the oldest body is the one dropped")
}

func TestRefusedStoreWritesOneFilePerRefusalNamedByCaseTurnAndStatus(t *testing.T) {
	dir := t.TempDir()
	store := newRefusedStore(dir)
	first := []byte(`{"model":"m","messages":[{"role":"user","content":"one"}]}`)
	second := []byte(`{"model":"m","messages":[{"role":"user","content":"two"}]}`)
	src := mapSource{hashOf(first): first, hashOf(second): second}
	attempts := []ports.AgentAttempt{
		{Kind: ports.ModelErrorServer, Status: 500, RequestHash: hashOf(first)},
		{Kind: ports.ModelErrorInvalid, Status: 400, RequestHash: hashOf(second)},
	}

	paths, err := store.save("fixture-refunds", 1, attempts, src)
	require.NoError(t, err)
	require.Equal(t, []string{
		filepath.Join(dir, "fixture-refunds-turn1-500.json"),
		filepath.Join(dir, "fixture-refunds-turn1-400.json"),
	}, paths)

	for i, want := range [][]byte{first, second} {
		got, err := os.ReadFile(paths[i])
		require.NoError(t, err)
		require.Equal(t, want, got)
		require.NotContains(t, string(got), testKey)
		require.NotContains(t, string(got), "Authorization")
		info, err := os.Stat(paths[i])
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "a saved body is private")
	}
}

func TestRefusedStoreIndexListsEachRefusal(t *testing.T) {
	dir := t.TempDir()
	store := newRefusedStore(dir)
	body := []byte(`{"model":"m","messages":[]}`)
	hash := hashOf(body)
	src := mapSource{hash: body}

	_, err := store.save("fixture-refunds", 1, []ports.AgentAttempt{
		{Kind: ports.ModelErrorServer, Status: 500, RequestHash: hash},
		{Kind: ports.ModelErrorServer, Status: 500, RequestHash: hash},
	}, src)
	require.NoError(t, err)
	_, err = store.save("testify-1207", 3, []ports.AgentAttempt{
		{Kind: ports.ModelErrorInvalid, Status: 400, RequestHash: hash},
	}, src)
	require.NoError(t, err)

	raw, err := os.ReadFile(filepath.Join(dir, refusedIndexName))
	require.NoError(t, err)
	var index refusedIndex
	require.NoError(t, json.Unmarshal(raw, &index))
	require.Contains(t, index.Note, "no key")
	require.Len(t, index.Refusals, 2)

	first := index.Refusals[0]
	require.Equal(t, "fixture-refunds", first.Case)
	require.Equal(t, 1, first.Turn)
	require.Equal(t, 500, first.Status)
	require.Equal(t, "server", first.Kind)
	require.Equal(t, 2, first.Attempts, "one file per refusal: the retry of the same body adds an attempt")
	require.Equal(t, len(body), first.Bytes)
	require.Equal(t, hash, first.SHA256)
	require.FileExists(t, filepath.Join(dir, first.File))

	second := index.Refusals[1]
	require.Equal(t, "testify-1207", second.Case)
	require.Equal(t, 400, second.Status)
	require.Equal(t, "invalid", second.Kind)
	require.Equal(t, 1, second.Attempts)
}

func TestRefusedStoreWritesNothingWithoutADirectory(t *testing.T) {
	store := newRefusedStore("")
	body := []byte(`{"model":"m","messages":[]}`)
	paths, err := store.save("case", 1, []ports.AgentAttempt{{Status: 500, RequestHash: hashOf(body)}},
		mapSource{hashOf(body): body})
	require.NoError(t, err)
	require.Equal(t, []string{""}, paths)
}

func TestRefusedStoreDoesNotSaveABodyPastItsSizeBound(t *testing.T) {
	dir := t.TempDir()
	store := newRefusedStore(dir)
	big := bytes.Repeat([]byte("x"), maxRefusedBodyBytes+1)
	paths, err := store.save("case", 1, []ports.AgentAttempt{{Status: 500, RequestHash: hashOf(big)}},
		mapSource{hashOf(big): big})
	require.NoError(t, err)
	require.Equal(t, []string{""}, paths, "a body past the bound is not saved")
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, e := range entries {
		require.NotContains(t, e.Name(), "turn1", "no body file is written")
	}
}

func TestRefusedStoreSkipsABodyTheRecorderDoesNotHold(t *testing.T) {
	dir := t.TempDir()
	store := newRefusedStore(dir)
	paths, err := store.save("case", 2, []ports.AgentAttempt{{Status: 500, RequestHash: hashOf([]byte("gone"))}}, mapSource{})
	require.NoError(t, err)
	require.Equal(t, []string{""}, paths)
}

func TestRefusedStoreKeepsTheIndexOfARunBeforeIt(t *testing.T) {
	// The default folder is under the OS temp directory and survives a run, so a second run must not drop the entries
	// that name files already there.
	dir := t.TempDir()
	body := []byte(`{"model":"m","messages":[]}`)
	src := mapSource{hashOf(body): body}
	first := newRefusedStore(dir)
	_, err := first.save("case-a", 1, []ports.AgentAttempt{{Status: 500, RequestHash: hashOf(body)}}, src)
	require.NoError(t, err)

	second := newRefusedStore(dir)
	_, err = second.save("case-b", 2, []ports.AgentAttempt{{Status: 400, RequestHash: hashOf(body)}}, src)
	require.NoError(t, err)

	raw, err := os.ReadFile(filepath.Join(dir, refusedIndexName))
	require.NoError(t, err)
	var index refusedIndex
	require.NoError(t, json.Unmarshal(raw, &index))
	require.Len(t, index.Refusals, 2, "the earlier run's entry stays")
	require.Equal(t, "case-a", index.Refusals[0].Case)
	require.Equal(t, "case-b", index.Refusals[1].Case)
}

func TestRefusedStoreKeepsBothBodiesOfOneCallAndStatusApart(t *testing.T) {
	// The tool_choice fallback sends a second body on the same call: the two files must not overwrite each other.
	dir := t.TempDir()
	store := newRefusedStore(dir)
	first := []byte(`{"model":"m","messages":[],"tool_choice":"required"}`)
	second := []byte(`{"model":"m","messages":[],"tool_choice":"auto"}`)
	src := mapSource{hashOf(first): first, hashOf(second): second}

	p1, err := store.save("case", 1, []ports.AgentAttempt{
		{Kind: ports.ModelErrorInvalid, Status: 400, RequestHash: hashOf(first)}}, src)
	require.NoError(t, err)
	p2, err := store.save("case", 1, []ports.AgentAttempt{
		{Kind: ports.ModelErrorServer, Status: 500, RequestHash: hashOf(second)}}, src)
	require.NoError(t, err)
	require.NotEqual(t, p1[0], p2[0], "two bodies never share one file")
	require.FileExists(t, p1[0])
	require.FileExists(t, p2[0])

	raw, err := os.ReadFile(filepath.Join(dir, refusedIndexName))
	require.NoError(t, err)
	var index refusedIndex
	require.NoError(t, json.Unmarshal(raw, &index))
	require.Len(t, index.Refusals, 2)
}

func TestRecordRefusedNamesTheSavedFileInTheFailedCall(t *testing.T) {
	dir := t.TempDir()
	body := []byte(`{"model":"m","messages":[]}`)
	hash := hashOf(body)
	model := liveChat{bodies: recorderWith(hash, body)}
	rc := reviewCase{Turns: 4, FailedCall: []attemptRecord{{Status: 500, RequestSHA256: hash}}}

	rc.recordRefused(newRefusedStore(dir), "fixture-refunds", []ports.AgentAttempt{
		{Kind: ports.ModelErrorServer, Status: 500, RequestHash: hash}}, model)

	require.Empty(t, rc.SaveError)
	require.Equal(t, filepath.Join(dir, "fixture-refunds-turn5-500.json"), rc.FailedCall[0].Saved,
		"the turn is the call's place in the case")
}

func TestRecordRefusedWritesNothingForAModelWithoutBodies(t *testing.T) {
	dir := t.TempDir()
	rc := reviewCase{FailedCall: []attemptRecord{{Status: 500}}}
	rc.recordRefused(newRefusedStore(dir), "case", []ports.AgentAttempt{{Status: 500}}, &countingModel{})
	require.Empty(t, rc.FailedCall[0].Saved, "a scripted model keeps no body")
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Empty(t, entries)
}

// recorderWith is a recorder that holds one body, for a test that does not go through HTTP.
func recorderWith(hash string, body []byte) *bodyRecorder {
	rec := newBodyRecorder(http.DefaultTransport)
	rec.byKey[hash] = body
	return rec
}

func TestSafeNameKeepsACaseIDInsideTheDirectory(t *testing.T) {
	require.Equal(t, "fixture-refunds", safeName("fixture-refunds"))
	require.Equal(t, "a-b-c", safeName("../a/b c"))
	require.Equal(t, "case", safeName(""))
}

func TestDefaultRefusedDirIsUnderTheTemporaryDirectory(t *testing.T) {
	require.Equal(t, filepath.Join(os.TempDir(), savedRefusedDirName), defaultRefusedDir())
}
