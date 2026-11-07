package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

const claudeSample = `{"type":"user","sessionId":"s1","timestamp":"2026-10-01T10:00:00Z","cwd":"/r/.claude/worktrees/w","message":{"role":"user","content":"add a refund cap please"}}
{"type":"assistant","sessionId":"s1","timestamp":"2026-10-01T10:01:00Z","cwd":"/r/.claude/worktrees/w","message":{"role":"assistant","content":[{"type":"thinking","thinking":"secret plan"},{"type":"text","text":"Adding it."},{"type":"tool_use","name":"Edit","input":{"file_path":"/r/.claude/worktrees/w/internal/pay/refund.go"}}]}}
{"type":"user","sessionId":"s1","timestamp":"2026-10-01T10:02:00Z","cwd":"/r/.claude/worktrees/w","message":{"role":"user","content":[{"type":"tool_result","content":"TOOL OUTPUT"}]}}
{"type":"assistant","sessionId":"s1","timestamp":"2026-10-01T10:03:00Z","cwd":"/r/.claude/worktrees/w","message":{"role":"assistant","content":[{"type":"tool_use","name":"Read","input":{"file_path":"/etc/passwd"}},{"type":"text","text":"Done."}]}}
{"type":"user","sessionId":"s1","isSidechain":true,"timestamp":"2026-10-01T10:04:00Z","cwd":"/r","message":{"role":"user","content":"side agent prompt text here"}}
{"type":"user","sessionId":"s1","timestamp":"2026-10-01T10:05:00Z","cwd":"/r/.claude/worktrees/w","message":{"role":"user","content":[{"type":"text","text":"no, you forgot earlier refunds"}]}}
{"type":"pr-link","sessionId":"s1","prNumber":42,"prUrl":"https://github.com/o/r/pull/42"}
{"type":"pr-link","sessionId":"s1","prNumber":42,"prUrl":"https://github.com/o/r/pull/42"}
`

func TestEncodeProjectPathMatchesClaudeFolderNames(t *testing.T) {
	require.Equal(t, "-Volumes-SSD-flip-switch-x--claude-worktrees-a", encodeProjectPath("/Volumes/SSD/flip_switch_x/.claude/worktrees/a"))
}

func TestParseClaudeSessionKeepsTextAndRelativeFilesOnly(t *testing.T) {
	got := parseClaudeSession(strings.NewReader(claudeSample), "/r", "fallback")
	require.Len(t, got.Links, 1, "a repeated pr-link record is kept once")
	require.Equal(t, 42, got.Links[0].Number)
	require.Len(t, got.Turns, 4)
	user, asst, side, correction := got.Turns[0], got.Turns[1], got.Turns[2], got.Turns[3]
	require.Equal(t, "user", user.Role)
	require.Equal(t, "2026-10-01", user.Date)
	require.Equal(t, "assistant", asst.Role)
	require.Equal(t, []string{"internal/pay/refund.go"}, asst.Files, "paths are relative, and paths outside the repository are dropped")
	require.Contains(t, asst.Text, "Adding it.")
	require.Contains(t, asst.Text, "Done.", "records of one tool loop form one turn")
	require.NotContains(t, asst.Text, "secret plan")
	require.NotContains(t, asst.Text, "TOOL OUTPUT")
	require.True(t, side.Sidechain)
	require.Equal(t, "no, you forgot earlier refunds", correction.Text)
}

func TestParseClaudeSessionSkipsOtherProjects(t *testing.T) {
	line := `{"type":"user","sessionId":"s","cwd":"/elsewhere","message":{"role":"user","content":"always do the thing"}}` + "\n"
	require.Empty(t, parseClaudeSession(strings.NewReader(line), "/r", "s").Turns)
}

func TestParseCodexSessionUsesTheMetaCwd(t *testing.T) {
	in := `{"type":"session_meta","payload":{"id":"c1","cwd":"/r/sub"}}
{"type":"response_item","timestamp":"2026-10-02T00:00:00Z","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"never skip the audit log"}]}}
{"type":"response_item","payload":{"type":"function_call","name":"shell"}}
{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Understood."}]}}
`
	got := parseCodexSession(strings.NewReader(in), "/r", "f")
	require.Len(t, got.Turns, 2)
	require.Equal(t, "c1", got.Turns[0].Session)
	require.Empty(t, parseCodexSession(strings.NewReader(in), "/other", "f").Turns)
}

func TestRefixLeadFindsAFileFixedTwiceInFourteenDays(t *testing.T) {
	out := "\x1ea1\t1000000\tfix cap\ninternal/pay/refund.go\nREADME.md\n" +
		"\x1eb2\t1100000\tfix cap again\ninternal/pay/refund.go\n" +
		"\x1ec3\t9000000\tfix other\ninternal/x/x.go\n"
	commits := parseCommits(out)
	require.Len(t, commits, 3)
	leads := refixLeads(commits)
	require.Len(t, leads, 1)
	require.Equal(t, "internal/pay/refund.go", leads[0].Text)
	require.Equal(t, "b2", leads[0].Ref)
	require.Equal(t, []string{"internal/pay/refund.go"}, codeFiles(commits[0].Files))
}

func TestCheckFlagsBrokenSecretAndVagueCards(t *testing.T) {
	good := "---\napplies_to: [\"internal/pay/**\"]\ntriggers: [\"refund(\"]\ncheck: \"Does the cap count past refunds?\"\nkind: invariant\nstatus: active\n---\nbody"
	require.Empty(t, checkCardText("a.md", good).problems)
	require.Empty(t, checkCardText("a.md", good).warnings)

	require.NotEmpty(t, checkCardText("b.md", "---\napplies_to: [oops\n---\nx").problems)
	require.NotEmpty(t, checkCardText("c.md", "mail bob@example.com about it").problems)
	short := "---\ntriggers: [\"ab\"]\n---\nx"
	require.NotEmpty(t, checkCardText("d.md", short).problems, "a trigger shorter than 3 characters would match everything")
	bad := "---\nstatus: maybe\n---\nx"
	require.NotEmpty(t, checkCardText("e.md", bad).problems)

	vague := checkCardText("f.md", "---\nkind: judgement\n---\nI care about quality")
	require.Empty(t, vague.problems)
	require.NotEmpty(t, vague.warnings)
	noCheck := checkCardText("g.md", "---\napplies_to: [\"a/**\"]\nkind: invariant\n---\nx")
	require.NotEmpty(t, noCheck.warnings)
}

func TestSetStatusRewritesOrAddsTheLineOnly(t *testing.T) {
	card := "---\napplies_to: [\"a/**\"]\nstatus: proposed\nkind: invariant\n---\nbody\n---\nkeep"
	require.Equal(t, "---\napplies_to: [\"a/**\"]\nstatus: active\nkind: invariant\n---\nbody\n---\nkeep", setStatus(card, "active"))
	require.Equal(t, "---\nkind: x\nstatus: rejected\n---\nb", setStatus("---\nkind: x\n---\nb", "rejected"))
	require.Equal(t, "---\nstatus: active\n---\nplain", setStatus("plain", "active"))
}

type fakeCardServer struct {
	mu    sync.Mutex
	cards map[string]string
	auth  string
}

func (f *fakeCardServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.auth = r.Header.Get("Authorization")
	name := strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, "/cards"), "/")
	switch {
	case r.Method == http.MethodGet && name == "":
		_, _ = io.WriteString(w, `{"cards":["new.md","old.md"]}`)
	case r.Method == http.MethodGet:
		_, _ = io.WriteString(w, f.cards[name])
	case r.Method == http.MethodPut:
		body, _ := io.ReadAll(r.Body)
		f.cards[name] = string(body)
		w.WriteHeader(http.StatusNoContent)
	}
}

func cardsTestEnv(t *testing.T, stdin string) (cardsEnv, *bytes.Buffer) {
	t.Helper()
	var out bytes.Buffer
	return cardsEnv{stdin: strings.NewReader(stdin), out: &out, errw: &out, client: http.DefaultClient}, &out
}

func writeTokenFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "token")
	require.NoError(t, os.WriteFile(path, []byte("tok-123-abc\n"), 0o600))
	return path
}

func TestPushChecksConfirmsAndUploads(t *testing.T) {
	srv := &fakeCardServer{cards: map[string]string{}}
	ts := httptest.NewServer(srv)
	defer ts.Close()
	dir := t.TempDir()
	good := "---\napplies_to: [\"a/**\"]\ncheck: \"Is x checked?\"\nkind: convention\n---\nbody"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.md"), []byte(good), 0o600))
	token := writeTokenFile(t)

	env, _ := cardsTestEnv(t, "no\n")
	require.Equal(t, 1, runCardsWith([]string{"push", "--server", ts.URL, "--token-file", token, "--dir", dir}, env))
	require.Empty(t, srv.cards, "without a yes nothing is uploaded")

	env, out := cardsTestEnv(t, "yes\n")
	require.Zero(t, runCardsWith([]string{"push", "--server", ts.URL, "--token-file", token, "--dir", dir}, env))
	require.Equal(t, good, srv.cards["a.md"])
	require.Equal(t, "Bearer tok-123-abc", srv.auth)
	require.NotContains(t, out.String(), "tok-123-abc", "the token is never printed")
	require.Contains(t, out.String(), "uploaded a.md")

	require.NoError(t, os.WriteFile(filepath.Join(dir, "b.md"), []byte("mail bob@example.com"), 0o600))
	env, _ = cardsTestEnv(t, "")
	srv.cards = map[string]string{}
	require.Equal(t, 1, runCardsWith([]string{"push", "--yes", "--server", ts.URL, "--token-file", token, "--dir", dir}, env))
	require.Empty(t, srv.cards, "one bad card stops the whole push")
}

func TestServerMustUseHTTPSExceptOnLocalhost(t *testing.T) {
	env, _ := cardsTestEnv(t, "")
	token := writeTokenFile(t)
	_, err := newCardsClient(remoteOpts{server: "http://example.com", tokenFile: token}, env)
	require.Error(t, err)
	_, err = newCardsClient(remoteOpts{server: "https://example.com", tokenFile: token}, env)
	require.NoError(t, err)
	_, err = newCardsClient(remoteOpts{server: "http://127.0.0.1:8080", tokenFile: token}, env)
	require.NoError(t, err)
	_, err = newCardsClient(remoteOpts{server: "https://example.com", tokenFile: filepath.Join(t.TempDir(), "missing")}, env)
	require.Error(t, err)
}

func TestInboxAcceptAndRejectRewriteStatus(t *testing.T) {
	srv := &fakeCardServer{cards: map[string]string{
		"new.md": "---\napplies_to: [\"a/**\"]\ncheck: \"Is x checked?\"\nstatus: proposed\n---\nlearned rule",
		"old.md": "---\nstatus: active\n---\nold rule",
	}}
	ts := httptest.NewServer(srv)
	defer ts.Close()
	token := writeTokenFile(t)
	base := []string{"--server", ts.URL, "--token-file", token}

	env, out := cardsTestEnv(t, "")
	require.Zero(t, runCardsWith(append([]string{"inbox"}, base...), env))
	require.Contains(t, out.String(), "learned rule")
	require.NotContains(t, out.String(), "old rule")

	env, _ = cardsTestEnv(t, "")
	require.Zero(t, runCardsWith(append([]string{"accept", "new.md"}, base...), env))
	require.Contains(t, srv.cards["new.md"], "status: active")
	require.Contains(t, srv.cards["new.md"], "learned rule")

	env, _ = cardsTestEnv(t, "")
	require.Zero(t, runCardsWith(append([]string{"reject", "new.md"}, base...), env))
	require.Contains(t, srv.cards["new.md"], "status: rejected")
}

func TestPullKeepsDifferingLocalCardsUnlessForced(t *testing.T) {
	srv := &fakeCardServer{cards: map[string]string{"new.md": "server text", "old.md": "old"}}
	ts := httptest.NewServer(srv)
	defer ts.Close()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "new.md"), []byte("local edit"), 0o600))
	args := []string{"pull", "--server", ts.URL, "--token-file", writeTokenFile(t), "--dir", dir}

	env, _ := cardsTestEnv(t, "")
	require.Zero(t, runCardsWith(args, env))
	local, err := os.ReadFile(filepath.Join(dir, "new.md"))
	require.NoError(t, err)
	require.Equal(t, "local edit", string(local))
	fetched, err := os.ReadFile(filepath.Join(dir, "old.md"))
	require.NoError(t, err)
	require.Equal(t, "old", string(fetched))

	env, _ = cardsTestEnv(t, "")
	require.Zero(t, runCardsWith(append(args, "--force"), env))
	local, err = os.ReadFile(filepath.Join(dir, "new.md"))
	require.NoError(t, err)
	require.Equal(t, "server text", string(local))
}

func TestPreviewShowsTheCardsThatWakeAndTheChangeMap(t *testing.T) {
	dir := t.TempDir()
	write := func(name, text string) {
		path := filepath.Join(dir, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
		require.NoError(t, os.WriteFile(path, []byte(text), 0o600))
	}
	write("base/app/a.go", "package app\n\nvar Flag = \"dry-run\"\n")
	write("head/app/a.go", "package app\n\nvar Flag = \"preview\"\n")
	write("base/run.sh", "./app --dry-run\n")
	write("head/run.sh", "./app --dry-run\n")
	write("cards/priya/rule.md", "---\ntriggers: [\"preview\"]\ncheck: \"Does the script still work?\"\nkind: invariant\n---\nKeep the script working.\n")
	write("cards/priya/other.md", "---\napplies_to: [\"web/**\"]\nkind: invariant\n---\nA rule about something else.\n")
	write("change.diff", "--- a/app/a.go\n+++ b/app/a.go\n@@ -3,1 +3,1 @@\n-var Flag = \"dry-run\"\n+var Flag = \"preview\"\n")

	text, err := previewBrief(filepath.Join(dir, "base"), filepath.Join(dir, "head"), filepath.Join(dir, "change.diff"), filepath.Join(dir, "cards"), "priya")
	require.NoError(t, err)
	require.Contains(t, text, "CHECK: Does the script still work?")
	require.NotContains(t, text, "something else")
	require.Contains(t, text, "run.sh:1")
}
