package external_test

import (
	"bufio"
	"encoding/json"
	"os"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/external"
)

// The real t3code data. benchPath is the 411 inline bot comments that docs/product/research/13-t3code-bot-comments.md
// describes: one JSON line per comment, with the bot login, the reply flag and the label. bodiesPath holds the same
// 411 comments with the raw body text and the coordinates, so the parser is fed what GitHub gives and not what a
// reading of it kept.
const (
	benchPath  = "../../../testdata/bench/t3code-comments.jsonl"
	bodiesPath = "testdata/corpus/t3code-bodies.jsonl"
)

// benchLine is the part of one line of the bench file that this test uses.
type benchLine struct {
	PR        int    `json:"pr"`
	CommentID int64  `json:"comment_id"`
	Path      string `json:"path"`
	Bot       string `json:"bot"`
	IsReply   bool   `json:"is_reply"`
}

// bodyComment is one line of the bodies file, in the shape this package reads.
type bodyComment struct {
	CommentID         int64  `json:"comment_id"`
	CommitID          string `json:"commit_id"`
	OriginalCommitID  string `json:"original_commit_id"`
	Path              string `json:"path"`
	Line              int    `json:"line"`
	StartLine         int    `json:"start_line"`
	OriginalLine      int    `json:"original_line"`
	OriginalStartLine int    `json:"original_start_line"`
	Side              string `json:"side"`
	InReplyTo         int64  `json:"in_reply_to_id"`
	Outdated          bool   `json:"outdated"`
	Login             string `json:"login"`
	Type              string `json:"type"`
	Body              string `json:"body"`
}

// comment is the body file line as the API would give it.
func (b bodyComment) comment() external.Comment {
	return external.Comment{
		ID: b.CommentID, Body: b.Body, Path: b.Path, Line: b.Line, StartLine: b.StartLine,
		OriginalLine: b.OriginalLine, OriginalStartLine: b.OriginalStartLine, Side: b.Side,
		CommitID: b.CommitID, OriginalCommitID: b.OriginalCommitID, Outdated: b.Outdated,
		InReplyTo: b.InReplyTo, UserLogin: b.Login, UserType: b.Type,
	}
}

// tally is what one bot produced in one run of the corpus.
type tally struct {
	top, candidates, dropped, style int
	ignored                         map[external.IgnoreReason]int
}

// TestRealBotCorpus parses every line of the real t3code data and keeps four promises: no line panics, every candidate
// is well formed, a reply is never a candidate, and at least 90% of the top-level bot comments become candidates. The
// per-bot counts are logged, so a format change by a tool shows as a number and not as a silent drop.
func TestRealBotCorpus(t *testing.T) {
	bodies := readBodies(t)
	bench := readBench(t)
	require.Len(t, bodies, 411, "the corpus is a frozen sample")
	require.Len(t, bench, 411, "the corpus is a frozen sample")

	per := map[string]*tally{}
	var top, candidates int
	for _, r := range bench {
		b, ok := bodies[r.CommentID]
		require.True(t, ok, "comment %d has no body", r.CommentID)
		got := external.PR{Head: b.CommitID, Sources: allSources()}.Ingest(b.comment())
		tl := per[r.Bot]
		if tl == nil {
			tl = &tally{ignored: map[external.IgnoreReason]int{}}
			per[r.Bot] = tl
		}
		if r.IsReply {
			require.Equal(t, external.OutcomeIgnored, got.Outcome, "comment %d is a reply, not a finding", r.CommentID)
			continue
		}
		tl.top++
		top++
		switch got.Outcome {
		case external.OutcomeCandidate:
			tl.candidates++
			candidates++
			requireCandidate(t, r.CommentID, got)
			if got.Candidate.Style {
				tl.style++
			}
		case external.OutcomeIgnored:
			tl.ignored[got.Ignore]++
		case external.OutcomeDropped:
			tl.dropped++
		}
	}
	require.Equal(t, 383, top, "the corpus has 383 top-level comments")

	for _, login := range sortedBots(per) {
		tl := per[login]
		t.Logf("%-28s top %3d  candidate %3d  ignored %3d %v  dropped %3d  style %d",
			login, tl.top, tl.candidates, tl.top-tl.candidates-tl.dropped, tl.ignored, tl.dropped, tl.style)
	}
	t.Logf("%-28s top %3d  candidate %3d (%.1f%%)", "TOTAL", top, candidates, 100*float64(candidates)/float64(top))
	require.GreaterOrEqual(t, candidates*10, top*9, "at least 90%% of the top-level bot comments must become candidates")
}

// requireCandidate checks the invariant that every candidate holds, whatever the tool wrote.
func requireCandidate(t *testing.T, id int64, got external.Result) {
	t.Helper()
	n := len([]rune(got.Candidate.Claim))
	require.GreaterOrEqual(t, n, 10, "comment %d: claim %q", id, got.Candidate.Claim)
	require.LessOrEqual(t, n, 600, "comment %d", id)
	require.True(t, got.Candidate.Severity.Valid(), "comment %d: severity %q", id, got.Candidate.Severity)
	require.GreaterOrEqual(t, got.Candidate.StartLine, 1, "comment %d", id)
	require.LessOrEqual(t, got.Candidate.StartLine, got.Candidate.EndLine, "comment %d", id)
}

// readBench reads the bench file.
func readBench(t *testing.T) []benchLine {
	t.Helper()
	var out []benchLine
	scanJSONL(t, benchPath, func(line []byte) {
		var r benchLine
		require.NoError(t, json.Unmarshal(line, &r), "bench line %q", line)
		out = append(out, r)
	})
	return out
}

// readBodies reads the bodies file, in comment id order.
func readBodies(t *testing.T) map[int64]bodyComment {
	t.Helper()
	out := map[int64]bodyComment{}
	scanJSONL(t, bodiesPath, func(line []byte) {
		var b bodyComment
		require.NoError(t, json.Unmarshal(line, &b), "body line %q", line)
		out[b.CommentID] = b
	})
	return out
}

// scanJSONL calls fn for every non-empty line of path.
func scanJSONL(t *testing.T, path string, fn func(line []byte)) {
	t.Helper()
	f, err := os.Open(path)
	require.NoError(t, err)
	defer func() { require.NoError(t, f.Close()) }()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), external.MaxBodyBytes*4)
	for sc.Scan() {
		if len(sc.Bytes()) > 0 {
			fn(sc.Bytes())
		}
	}
	require.NoError(t, sc.Err())
}

// sortedBots is the logins in a stable order, so two runs log the same lines.
func sortedBots(m map[string]*tally) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}
