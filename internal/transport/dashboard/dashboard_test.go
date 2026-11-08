package dashboard_test

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	appdashboard "github.com/prathameshk421/flip_the_switch_hackathon/internal/app/dashboard"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/testkit/changelog"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/transport"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/transport/dashboard"
)

const (
	pay7      = id.ChangeID("PAY-7")
	pay8      = id.ChangeID("PAY-8")
	findingID = "fnd_01ARZ3NDEKTSV4RRFFQ69G5FAV"
	sha       = id.SHA("3f2a9c1d0b7e4a5f6c8d9e0a1b2c3d4e5f607182")
)

// pay7Log builds PAY-7 with eight events: the stored log in these tests.
func pay7Log(t testing.TB) *changelog.Log {
	return changelog.New(t, pay7).
		Add(change.EventChangeTriggered, changelog.Human("alice"),
			change.Triggered{Ticket: pay7, Repo: "owner/name", Assignee: "alice", Source: change.TriggerJira}).
		Add(change.EventRoomOpened, changelog.Agent("heimdall"), change.RoomOpened{RoomID: "room-7"}).
		Add(change.EventReviewersPicked, changelog.Human("alice"), change.ReviewersPicked{
			People: []change.ActorID{"reviewer-b", "reviewer-a"}, By: "alice", Method: change.PickDashboard,
		}).
		Add(change.EventReviewSubmitted, changelog.Agent("reviewer-a"), change.ReviewSubmitted{
			Reviewer: "reviewer-a", FindingRefs: []id.FindingRef{1}, PackDate: "2026-11-07",
		}).
		Add(change.EventReviewUnavailable, changelog.Agent("heimdall"), change.ReviewUnavailable{
			Reviewer: "reviewer-b", Reason: "no answer in time",
		}).
		Add(change.EventFindingFiled, changelog.Agent("reviewer-a"), change.FindingFiled{
			FindingID: findingID, Ref: 1, Tier: finding.TierE1, Severity: finding.SeverityHigh,
			State: finding.StateOpen, By: "reviewer-a",
		}).
		Add(change.EventQuestionAsked, changelog.Agent("heimdall"), change.QuestionAsked{
			QuestionID: "q-1", Refs: []string{"PAY-7 spec"}, Sender: "sender-1", MessageID: "msg-1",
		}).
		Add(change.EventGateEvaluated, changelog.Agent("heimdall"), change.GateEvaluated{
			OK: false, Reasons: []string{"F1 is open on the head commit"}, SHA: sha, TreeHash: sha, PolicyVersion: 1,
		})
}

// fullLog is the stored log of PAY-7: eight events.
func fullLog(t testing.TB) []change.Event {
	return pay7Log(t).Events()
}

// ninthEvent is the event that arrives after the stored log: an answer to the open question.
func ninthEvent(t testing.TB) change.Event {
	evs := pay7Log(t).Add(change.EventQuestionAnswered, changelog.Agent("heimdall"),
		change.QuestionAnswered{QuestionID: "q-1", Sender: "sender-1", MessageID: "msg-2", Answer: "yes, proceed"}).Events()
	return evs[len(evs)-1]
}

// logger discards output. Tests read the HTTP answers, not the log.
func logger() *slog.Logger { return slog.New(slog.DiscardHandler) }

// systemTicker is the ticker the server supplies. Tests may read the real clock.
func systemTicker(every time.Duration) (<-chan time.Time, func()) {
	t := time.NewTicker(every)
	return t.C, t.Stop
}

// serve starts the dashboard in front of the real router, with the given logs in the history.
func serve(t testing.TB, logs map[id.ChangeID][]change.Event, opts dashboard.Options) (*httptest.Server, *changelog.History) {
	t.Helper()
	h := changelog.NewHistory()
	for cid, evs := range logs {
		h.Put(cid, evs)
	}
	router, err := transport.NewRouter(logger())
	require.NoError(t, err)
	opts.NewTicker = systemTicker
	handler, err := dashboard.Mount(router, h, logger(), opts)
	require.NoError(t, err)
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)
	return ts, h
}

// reply is one HTTP answer. get reads and closes the body, so no test holds a response.
type reply struct {
	Status int
	Header http.Header
	Body   string
}

// get issues a GET with the test context and returns the answer.
func get(t testing.TB, ts *httptest.Server, path string) reply {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, ts.URL+path, nil)
	require.NoError(t, err)
	resp, err := ts.Client().Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return reply{Status: resp.StatusCode, Header: resp.Header, Body: string(body)}
}

func TestListPageShowsEveryChangeWithItsGate(t *testing.T) {
	ts, _ := serve(t, map[id.ChangeID][]change.Event{
		pay7: fullLog(t),
		pay8: changelog.New(t, pay8).Add(change.EventChangeTriggered, changelog.Human("bob"),
			change.Triggered{Ticket: pay8, Repo: "owner/name", Assignee: "bob", Source: change.TriggerJira}).Events(),
	}, dashboard.Options{})

	resp := get(t, ts, "/")
	require.Equal(t, http.StatusOK, resp.Status)
	require.True(t, strings.HasPrefix(resp.Header.Get("Content-Type"), "text/html"))
	require.Contains(t, resp.Body, `href="/changes/PAY-7"`)
	require.Contains(t, resp.Body, `href="/changes/PAY-8"`)
	require.Contains(t, resp.Body, "blocked")
	require.Contains(t, resp.Body, "not evaluated")
}

func TestChangePageShowsGateFindingsReviewersQuestionsAndTimeline(t *testing.T) {
	ts, _ := serve(t, map[id.ChangeID][]change.Event{pay7: fullLog(t)}, dashboard.Options{})

	resp := get(t, ts, "/changes/PAY-7")
	require.Equal(t, http.StatusOK, resp.Status)
	require.Contains(t, resp.Body, "F1 is open on the head commit")
	require.Contains(t, resp.Body, "reviewer-a")
	require.Contains(t, resp.Body, "submitted")
	require.Contains(t, resp.Body, "q-1")
	require.Contains(t, resp.Body, "gate.evaluated")
	require.Contains(t, resp.Body, "/sse/changes/PAY-7", "the page must open the live stream of its change")
	require.Contains(t, resp.Body, "/assets/dashboard.js")
	require.Contains(t, resp.Body, "/assets/dashboard.css")
}

func TestChangePageEscapesTextFromTheLog(t *testing.T) {
	evs := changelog.New(t, pay7).
		Add(change.EventChangeTriggered, changelog.Human("alice"),
			change.Triggered{Ticket: pay7, Repo: "owner/name", Assignee: "alice", Source: change.TriggerJira}).
		Add(change.EventGateEvaluated, changelog.Agent("heimdall"), change.GateEvaluated{
			OK: false, Reasons: []string{`<img src=x onerror="alert(1)">`}, SHA: sha, TreeHash: sha, PolicyVersion: 1,
		}).Events()
	ts, _ := serve(t, map[id.ChangeID][]change.Event{pay7: evs}, dashboard.Options{})

	resp := get(t, ts, "/changes/PAY-7")
	require.NotContains(t, resp.Body, `<img src=x`)
	require.Contains(t, resp.Body, "&lt;img src=x")
}

func TestUnknownChangeIsNotFound(t *testing.T) {
	ts, _ := serve(t, map[id.ChangeID][]change.Event{pay7: fullLog(t)}, dashboard.Options{})

	for _, path := range []string{"/changes/PAY-404", "/changes/not-a-change", "/changes/PAY-7/extra"} {
		resp := get(t, ts, path)
		require.Equal(t, http.StatusNotFound, resp.Status, path)
	}
	resp := get(t, ts, "/sse/changes/PAY-404")
	require.Equal(t, http.StatusNotFound, resp.Status)
}

func TestRootAcceptsOnlyGet(t *testing.T) {
	ts, _ := serve(t, map[id.ChangeID][]change.Event{pay7: fullLog(t)}, dashboard.Options{})

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, ts.URL+"/", nil)
	require.NoError(t, err)
	resp, err := ts.Client().Do(req)
	require.NoError(t, err)
	_ = resp.Body.Close()
	require.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
}

func TestAssetsAreServedFromTheBinary(t *testing.T) {
	ts, _ := serve(t, map[id.ChangeID][]change.Event{pay7: fullLog(t)}, dashboard.Options{})

	css := get(t, ts, "/assets/dashboard.css")
	require.Equal(t, http.StatusOK, css.Status)
	require.True(t, strings.HasPrefix(css.Header.Get("Content-Type"), "text/css"))
	require.NotEmpty(t, css.Body)

	js := get(t, ts, "/assets/dashboard.js")
	require.Equal(t, http.StatusOK, js.Status)
	require.Contains(t, js.Header.Get("Content-Type"), "javascript")
	require.NotEmpty(t, js.Body)

	for _, path := range []string{"/assets/missing.css", "/assets/../dashboard.go", "/assets/"} {
		resp := get(t, ts, path)
		require.Equal(t, http.StatusNotFound, resp.Status, path)
	}
}

func TestPagesSetAStrictContentPolicy(t *testing.T) {
	ts, _ := serve(t, map[id.ChangeID][]change.Event{pay7: fullLog(t)}, dashboard.Options{})

	resp := get(t, ts, "/changes/PAY-7")
	csp := resp.Header.Get("Content-Security-Policy")
	require.Contains(t, csp, "default-src 'none'")
	require.Contains(t, csp, "script-src 'self'")
	require.Equal(t, "nosniff", resp.Header.Get("X-Content-Type-Options"))
}

func TestOtherRoutesKeepTheirAnswers(t *testing.T) {
	ts, _ := serve(t, map[id.ChangeID][]change.Event{pay7: fullLog(t)}, dashboard.Options{})

	resp := get(t, ts, "/healthz")
	require.Equal(t, http.StatusOK, resp.Status)
	require.JSONEq(t, `{"status":"ok"}`, resp.Body)

	for _, path := range []string{"/mcp", "/healthz/"} {
		resp := get(t, ts, path)
		require.Equal(t, http.StatusNotFound, resp.Status, path)
	}
}

func TestMountRefusesNilHistoryLoggerAndTicker(t *testing.T) {
	router, err := transport.NewRouter(logger())
	require.NoError(t, err)
	good := dashboard.Options{NewTicker: systemTicker}

	_, err = dashboard.Mount(router, nil, logger(), good)
	require.Error(t, err)

	var nilHistory appdashboard.History
	_, err = dashboard.Mount(router, nilHistory, logger(), good)
	require.Error(t, err)

	history := changelog.NewHistory()
	_, err = dashboard.Mount(router, history, nil, good)
	require.Error(t, err)

	_, err = dashboard.Mount(router, history, logger(), dashboard.Options{})
	require.Error(t, err, "the server must supply the ticker")

	_, err = dashboard.Mount(router, history, logger(), dashboard.Options{NewTicker: systemTicker, PollEvery: -time.Second})
	require.Error(t, err)
}

// sseFrame is one server-sent event as the client reads it.
type sseFrame struct {
	ID    string
	Event string
	Data  string
}

// frames reads frames from r in a goroutine and sends each complete frame on the returned channel. Comment lines
// (keep-alives) are skipped.
func frames(r io.Reader) <-chan sseFrame {
	out := make(chan sseFrame, 32)
	go func() {
		defer close(out)
		sc := bufio.NewScanner(r)
		var cur sseFrame
		for sc.Scan() {
			line := sc.Text()
			switch {
			case line == "":
				if cur != (sseFrame{}) {
					out <- cur
				}
				cur = sseFrame{}
			case strings.HasPrefix(line, ":"):
			case strings.HasPrefix(line, "id: "):
				cur.ID = strings.TrimPrefix(line, "id: ")
			case strings.HasPrefix(line, "event: "):
				cur.Event = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				cur.Data = strings.TrimPrefix(line, "data: ")
			}
		}
	}()
	return out
}

// nextFrame waits for one frame or fails the test after five seconds.
func nextFrame(t *testing.T, ch <-chan sseFrame) sseFrame {
	t.Helper()
	select {
	case f, ok := <-ch:
		require.True(t, ok, "stream closed before a frame arrived")
		return f
	case <-time.After(5 * time.Second):
		t.Fatal("no event within 5 seconds")
		return sseFrame{}
	}
}

// openStream opens a live stream for path and returns its frames. The response closes when the test ends.
func openStream(t *testing.T, ts *httptest.Server, path string, header map[string]string) <-chan sseFrame {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+path, nil)
	require.NoError(t, err)
	req.Header.Set("Accept", "text/event-stream")
	for k, v := range header {
		req.Header.Set(k, v)
	}
	resp, err := ts.Client().Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))
	return frames(resp.Body)
}

func TestLiveStreamSendsOnlyEventsAfterTheRenderedVersion(t *testing.T) {
	ts, h := serve(t, map[id.ChangeID][]change.Event{pay7: fullLog(t)}, dashboard.Options{PollEvery: 10 * time.Millisecond})

	ch := openStream(t, ts, "/sse/changes/PAY-7?after=8", nil)
	h.Append(pay7, ninthEvent(t))

	f := nextFrame(t, ch)
	require.Equal(t, "9", f.ID, "the first frame must be version 9, not a replay of versions 1 to 8")
	require.Equal(t, "change", f.Event)

	var item struct {
		Version int64  `json:"version"`
		Type    string `json:"type"`
		State   string `json:"state"`
		Gate    string `json:"gate"`
	}
	require.NoError(t, json.Unmarshal([]byte(f.Data), &item))
	require.Equal(t, int64(9), item.Version)
	require.Equal(t, string(change.EventQuestionAnswered), item.Type)
	require.Equal(t, "blocked", item.Gate)
}

func TestLiveStreamHonoursLastEventIDOnReconnect(t *testing.T) {
	ts, h := serve(t, map[id.ChangeID][]change.Event{pay7: fullLog(t)}, dashboard.Options{PollEvery: 10 * time.Millisecond})

	ch := openStream(t, ts, "/sse/changes/PAY-7", map[string]string{"Last-Event-ID": "8"})
	h.Append(pay7, ninthEvent(t))

	f := nextFrame(t, ch)
	require.Equal(t, "9", f.ID)
}

func TestLiveStreamWithoutAnyVersionStartsAtTheFirstEvent(t *testing.T) {
	ts, _ := serve(t, map[id.ChangeID][]change.Event{pay7: fullLog(t)}, dashboard.Options{PollEvery: 10 * time.Millisecond})

	ch := openStream(t, ts, "/sse/changes/PAY-7", nil)
	f := nextFrame(t, ch)
	require.Equal(t, "1", f.ID)
}

func TestLiveStreamRefusesABadVersion(t *testing.T) {
	ts, _ := serve(t, map[id.ChangeID][]change.Event{pay7: fullLog(t)}, dashboard.Options{})

	for _, path := range []string{"/sse/changes/PAY-7?after=-1", "/sse/changes/PAY-7?after=abc"} {
		resp := get(t, ts, path)
		require.Equal(t, http.StatusBadRequest, resp.Status, path)
	}
}

func TestReportPageAndSpecAreServedUnderTheStrictPolicy(t *testing.T) {
	ts, _ := serve(t, map[id.ChangeID][]change.Event{pay7: fullLog(t)}, dashboard.Options{})

	page := get(t, ts, "/changes/PAY-7/report")
	require.Equal(t, http.StatusOK, page.Status)
	require.Contains(t, page.Body, `src="/assets/report.js"`)
	require.Contains(t, page.Header.Get("Content-Security-Policy"), "script-src 'self'")
	require.NotContains(t, page.Body, "<script>", "the page holds no inline script")

	spec := get(t, ts, "/changes/PAY-7/report.json")
	require.Equal(t, http.StatusOK, spec.Status)
	require.Equal(t, "application/json", spec.Header.Get("Content-Type"))
	var decoded struct {
		Root     string                     `json:"root"`
		Elements map[string]json.RawMessage `json:"elements"`
	}
	require.NoError(t, json.Unmarshal([]byte(spec.Body), &decoded))
	require.Contains(t, decoded.Elements, decoded.Root)

	require.Equal(t, http.StatusNotFound, get(t, ts, "/changes/PAY-404/report").Status)
	require.Equal(t, http.StatusNotFound, get(t, ts, "/changes/PAY-404/report.json").Status)
	for _, asset := range []string{"report.js", "report.css"} {
		require.Equal(t, http.StatusOK, get(t, ts, "/assets/"+asset).Status, asset)
	}
}
