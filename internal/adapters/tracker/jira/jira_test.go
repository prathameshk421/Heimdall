package jira

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/tracker/internal/poll"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

const (
	emailValue = "bot@example.com"
	tokenValue = "tok-SECRET-123"
	ana        = `{"accountId":"557058:ana","accountType":"atlassian"}`
)

var now = time.Date(2026, 11, 4, 10, 0, 0, 0, time.UTC)

// clock is a ports.Clock a test can move between polls.
type clock struct{ at time.Time }

func (c *clock) Now() time.Time { return c.at }

type fakeSecrets map[string]string

func (f fakeSecrets) Secret(name string) (string, error) {
	v, ok := f[name]
	if !ok {
		return "", fmt.Errorf("secret %q is not set", name)
	}
	return v, nil
}

// recorded is one request the fake Jira received.
type recorded struct {
	Path  string
	Query url.Values
	Auth  string
}

// fake is an httptest Jira that records every request and answers with handle.
type fake struct {
	srv  *httptest.Server
	mu   sync.Mutex
	reqs []recorded
}

func newFake(t *testing.T, handle http.HandlerFunc) *fake {
	t.Helper()
	f := &fake{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.reqs = append(f.reqs, recorded{Path: r.URL.Path, Query: r.URL.Query(), Auth: r.Header.Get("Authorization")})
		f.mu.Unlock()
		handle(w, r)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fake) requests() []recorded {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]recorded(nil), f.reqs...)
}

// waits records every pause the client asks for and never really sleeps. stop makes the next pause fail with the
// context's error, as a cancelled call would.
type waits struct {
	mu   sync.Mutex
	d    []time.Duration
	stop bool
}

func (w *waits) Wait(ctx context.Context, d time.Duration) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.stop {
		return context.Canceled
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	w.d = append(w.d, d)
	return nil
}

func (w *waits) pauses() []time.Duration {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]time.Duration(nil), w.d...)
}

func newClient(t *testing.T, f *fake, c *clock) *Client {
	t.Helper()
	return newClientWait(t, f, c, &waits{})
}

func newClientWait(t *testing.T, f *fake, c *clock, w *waits) *Client {
	t.Helper()
	cl, err := New(Config{BaseURL: f.srv.URL, Project: "KAN", EmailSecret: "jira_email", TokenSecret: "jira_api_token"},
		fakeSecrets{"jira_email": emailValue, "jira_api_token": tokenValue}, c, w.Wait, f.srv.Client())
	require.NoError(t, err)
	return cl
}

func reply(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}

func history(changelogID, created, field string) string {
	return fmt.Sprintf(`{"id":%q,"created":%q,"items":[{"field":%q,"fromString":"To Do","toString":"In Progress"}]}`,
		changelogID, created, field)
}

func issue(key, assignee string, histories ...string) string {
	return fmt.Sprintf(`{"key":%q,"fields":{"assignee":%s},"changelog":{"histories":[%s]}}`,
		key, assignee, strings.Join(histories, ","))
}

func search(isLast bool, next string, issues ...string) string {
	return fmt.Sprintf(`{"issues":[%s],"isLast":%t,"nextPageToken":%q}`, strings.Join(issues, ","), isLast, next)
}

func basic() string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(emailValue+":"+tokenValue))
}

func digest(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func TestChangesQueriesFromTheCursorMinusTheOverlap(t *testing.T) {
	f := newFake(t, func(w http.ResponseWriter, _ *http.Request) {
		reply(w, http.StatusOK, search(true, "", issue("KAN-1", ana, history("10042", "2026-11-04T09:55:00.000+0000", "status"))))
	})
	c := &clock{at: now}
	cl := newClient(t, f, c)

	_, next, err := cl.Changes(context.Background(), poll.Format(now.Add(-time.Hour)))

	require.NoError(t, err)
	reqs := f.requests()
	require.Len(t, reqs, 1)
	require.Equal(t, "/rest/api/3/search/jql", reqs[0].Path)
	jql := reqs[0].Query.Get("jql")
	require.Contains(t, jql, "project = KAN")
	require.Contains(t, jql, `status CHANGED TO "In Progress" AFTER "2026-11-04 08:50"`, "cursor 09:00 minus 10 minutes")
	require.Contains(t, jql, "assignee IS NOT EMPTY")
	require.Equal(t, basic(), reqs[0].Auth, "the API token goes in the header, never in the URL")
	require.Equal(t, poll.Format(now), next, "the new cursor is the clock time of this poll")
}

func TestAnEmptyCursorStartsOneOverlapBeforeNow(t *testing.T) {
	f := newFake(t, func(w http.ResponseWriter, _ *http.Request) {
		reply(w, http.StatusOK, search(true, ""))
	})
	cl := newClient(t, f, &clock{at: now})

	_, _, err := cl.Changes(context.Background(), "")

	require.NoError(t, err)
	reqs := f.requests()
	require.Len(t, reqs, 1)
	require.Contains(t, reqs[0].Query.Get("jql"), `AFTER "2026-11-04 09:50"`)
}

func TestOnlyHistoriesInsideTheWindowAreEmitted(t *testing.T) {
	f := newFake(t, func(w http.ResponseWriter, _ *http.Request) {
		reply(w, http.StatusOK, search(true, "", issue("KAN-1", ana,
			history("10041", "2026-11-04T08:40:00.000+0000", "status"),
			history("10042", "2026-11-04T09:55:00.000+0000", "status"))))
	})
	cl := newClient(t, f, &clock{at: now})

	changes, _, err := cl.Changes(context.Background(), poll.Format(now.Add(-time.Hour)))

	require.NoError(t, err)
	require.Len(t, changes, 1)
	require.Equal(t, id.ChangeID("KAN-1"), changes[0].Key)
	require.Equal(t, "10042", changes[0].ChangelogID)
	require.Equal(t, []string{"status"}, changes[0].Fields)
	require.True(t, changes[0].At.Equal(time.Date(2026, 11, 4, 9, 55, 0, 0, time.UTC)), "got %s", changes[0].At)
	require.NoError(t, changes[0].Validate())
}

func TestAnOverlapRepeatIsNotReportedTwiceButALateIndexedChangeIs(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	f := newFake(t, func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		if n == 1 {
			reply(w, http.StatusOK, search(true, "", issue("KAN-1", ana, history("10042", "2026-11-04T09:55:00.000+0000", "status"))))
			return
		}
		// The second poll sees the same history again (inside the overlap) and one indexed late.
		reply(w, http.StatusOK, search(true, "", issue("KAN-1", ana,
			history("10042", "2026-11-04T09:55:00.000+0000", "status"),
			history("10043", "2026-11-04T09:58:00.000+0000", "status"))))
	})
	c := &clock{at: now}
	cl := newClient(t, f, c)

	first, cursor, err := cl.Changes(context.Background(), "")
	require.NoError(t, err)
	require.Len(t, first, 1)

	c.at = now.Add(5 * time.Minute)
	second, _, err := cl.Changes(context.Background(), cursor)

	require.NoError(t, err)
	require.Len(t, second, 1, "the repeat of 10042 is dropped; the late 10043 is kept")
	require.Equal(t, "10043", second[0].ChangelogID)
}

func TestPaginationFollowsTheNextPageToken(t *testing.T) {
	f := newFake(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("nextPageToken") == "" {
			reply(w, http.StatusOK, search(false, "p2", issue("KAN-1", ana, history("1", "2026-11-04T09:55:00.000+0000", "status"))))
			return
		}
		reply(w, http.StatusOK, search(true, "", issue("KAN-2", ana, history("2", "2026-11-04T09:56:00.000+0000", "status"))))
	})
	cl := newClient(t, f, &clock{at: now})

	changes, _, err := cl.Changes(context.Background(), poll.Format(now.Add(-time.Hour)))

	require.NoError(t, err)
	require.Len(t, changes, 2)
	require.Equal(t, id.ChangeID("KAN-1"), changes[0].Key)
	require.Equal(t, id.ChangeID("KAN-2"), changes[1].Key)
	reqs := f.requests()
	require.Len(t, reqs, 2)
	require.Equal(t, "p2", reqs[1].Query.Get("nextPageToken"))
}

func TestChangedFieldNamesComeFromTheChangelogItems(t *testing.T) {
	f := newFake(t, func(w http.ResponseWriter, _ *http.Request) {
		reply(w, http.StatusOK, search(true, "", issue("KAN-1", ana, history("10044", "2026-11-04T09:57:00.000+0000", "assignee"))))
	})
	cl := newClient(t, f, &clock{at: now})

	changes, _, err := cl.Changes(context.Background(), poll.Format(now.Add(-time.Hour)))

	require.NoError(t, err)
	require.Len(t, changes, 1)
	require.Equal(t, []string{"assignee"}, changes[0].Fields)
}

func TestAppAndUnassignedIssuesAreSkipped(t *testing.T) {
	f := newFake(t, func(w http.ResponseWriter, _ *http.Request) {
		reply(w, http.StatusOK, search(true, "",
			issue("KAN-3", `{"accountId":"app:1","accountType":"app"}`, history("1", "2026-11-04T09:55:00.000+0000", "status")),
			issue("KAN-4", "null", history("2", "2026-11-04T09:55:00.000+0000", "status")),
			issue("KAN-5", ana, history("3", "2026-11-04T09:55:00.000+0000", "status"))))
	})
	cl := newClient(t, f, &clock{at: now})

	changes, _, err := cl.Changes(context.Background(), poll.Format(now.Add(-time.Hour)))

	require.NoError(t, err)
	require.Len(t, changes, 1)
	require.Equal(t, id.ChangeID("KAN-5"), changes[0].Key)
}

func TestATrackerKeyThatIsNotATicketKeyIsRefused(t *testing.T) {
	f := newFake(t, func(w http.ResponseWriter, _ *http.Request) {
		reply(w, http.StatusOK, search(true, "", issue("kan-1", ana, history("1", "2026-11-04T09:55:00.000+0000", "status"))))
	})
	cl := newClient(t, f, &clock{at: now})

	_, _, err := cl.Changes(context.Background(), poll.Format(now.Add(-time.Hour)))

	require.Error(t, err)
}

const kan7 = `{"key":"KAN-7","fields":{"summary":"Add refunds","status":{"name":"In Progress"},` +
	`"assignee":{"accountId":"557058:ana","accountType":"atlassian"},"description":` +
	`{"type":"doc","version":1,"content":[` +
	`{"type":"paragraph","content":[{"type":"text","text":"Merchants can refund."}]},` +
	`{"type":"heading","attrs":{"level":2},"content":[{"type":"text","text":"Acceptance criteria"}]},` +
	`{"type":"orderedList","content":[` +
	`{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"A refund cannot exceed the payment."}]}]},` +
	`{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"Only the merchant can refund."}]}]}` +
	`]}]}}}`

func TestTicketParsesTitleStatusAssigneeAndCriteriaFromADF(t *testing.T) {
	f := newFake(t, func(w http.ResponseWriter, _ *http.Request) {
		reply(w, http.StatusOK, kan7)
	})
	cl := newClient(t, f, &clock{at: now})

	got, err := cl.Ticket(context.Background(), id.ChangeID("KAN-7"))

	require.NoError(t, err)
	reqs := f.requests()
	require.Len(t, reqs, 1)
	require.Equal(t, "/rest/api/3/issue/KAN-7", reqs[0].Path)
	require.Equal(t, basic(), reqs[0].Auth)
	require.Equal(t, id.ChangeID("KAN-7"), got.Key)
	require.Equal(t, "Add refunds", got.Title)
	require.Equal(t, "In Progress", got.Status)
	require.Equal(t, "557058:ana", string(got.Assignee))
	require.Len(t, got.Criteria, 2)
	require.Equal(t, "AC1", got.Criteria[0].ID)
	require.Equal(t, "A refund cannot exceed the payment.", got.Criteria[0].Text)
	require.Equal(t, digest(got.Criteria[0].Text), got.Criteria[0].Hash)
	require.Equal(t, "AC2", got.Criteria[1].ID)
	require.NoError(t, got.Validate())
}

func TestATicketWithANullDescriptionHasNoCriteria(t *testing.T) {
	f := newFake(t, func(w http.ResponseWriter, _ *http.Request) {
		reply(w, http.StatusOK, `{"key":"KAN-8","fields":{"summary":"Bare","status":{"name":"To Do"},`+
			`"assignee":{"accountId":"557058:ana","accountType":"atlassian"},"description":null}}`)
	})
	cl := newClient(t, f, &clock{at: now})

	got, err := cl.Ticket(context.Background(), id.ChangeID("KAN-8"))

	require.NoError(t, err)
	require.Empty(t, got.Criteria)
}

func TestTicketWithoutAnAssigneeIsRefused(t *testing.T) {
	f := newFake(t, func(w http.ResponseWriter, _ *http.Request) {
		reply(w, http.StatusOK, `{"key":"KAN-9","fields":{"summary":"Orphan","status":{"name":"To Do"},"assignee":null,"description":null}}`)
	})
	cl := newClient(t, f, &clock{at: now})

	_, err := cl.Ticket(context.Background(), id.ChangeID("KAN-9"))

	require.Error(t, err)
}

func TestKeysThatAreNotTicketKeysNeverReachJira(t *testing.T) {
	f := newFake(t, func(w http.ResponseWriter, _ *http.Request) {
		reply(w, http.StatusOK, kan7)
	})
	cl := newClient(t, f, &clock{at: now})

	for _, key := range []string{"../KAN-1", "kan-7", "KAN-7/../x", "KAN-7?fields=x", ""} {
		_, err := cl.Ticket(context.Background(), id.ChangeID(key))
		require.Error(t, err, key)
	}
	require.Empty(t, f.requests(), "a malformed key is refused before any request")
}

func TestHTTPStatusesMapToPortErrors(t *testing.T) {
	cases := []struct {
		status int
		want   error
	}{
		{http.StatusNotFound, ports.ErrNotFound},
		{http.StatusUnauthorized, ports.ErrUnauthorized},
		{http.StatusForbidden, ports.ErrUnauthorized},
		{http.StatusTooManyRequests, ports.ErrUnavailable},
		{http.StatusInternalServerError, ports.ErrUnavailable},
		{http.StatusServiceUnavailable, ports.ErrUnavailable},
	}
	for _, tc := range cases {
		f := newFake(t, func(w http.ResponseWriter, _ *http.Request) {
			reply(w, tc.status, `{"errorMessages":["nope"]}`)
		})
		cl := newClient(t, f, &clock{at: now})

		_, err := cl.Ticket(context.Background(), id.ChangeID("KAN-7"))

		require.ErrorIs(t, err, tc.want, "status %d", tc.status)
	}
}

func TestErrorsNeverCarryTheToken(t *testing.T) {
	f := newFake(t, func(w http.ResponseWriter, _ *http.Request) {
		reply(w, http.StatusUnauthorized, `{"errorMessages":["bad token"]}`)
	})
	cl := newClient(t, f, &clock{at: now})

	_, err := cl.Ticket(context.Background(), id.ChangeID("KAN-7"))
	require.Error(t, err)
	require.NotContains(t, err.Error(), tokenValue)
	require.NotContains(t, err.Error(), basic(), "the Basic credential is base64 of email:token")
	require.NotContains(t, err.Error(), strings.TrimPrefix(basic(), "Basic "))

	_, _, err = cl.Changes(context.Background(), "")
	require.Error(t, err)
	require.NotContains(t, err.Error(), tokenValue)
}

func TestNewRefusesBadConfigAndMissingSecretsWithoutPrintingThem(t *testing.T) {
	good := Config{BaseURL: "https://example.atlassian.net", Project: "KAN", EmailSecret: "jira_email", TokenSecret: "jira_api_token"}
	secrets := fakeSecrets{"jira_email": emailValue, "jira_api_token": tokenValue}
	cases := []struct {
		name    string
		mutate  func(c *Config)
		secrets Secrets
	}{
		{"no scheme", func(c *Config) { c.BaseURL = "example.atlassian.net" }, secrets},
		{"wrong scheme", func(c *Config) { c.BaseURL = "ftp://example.atlassian.net" }, secrets},
		{"lower-case project", func(c *Config) { c.Project = "kan" }, secrets},
		{"empty project", func(c *Config) { c.Project = "" }, secrets},
		{"token secret missing", func(_ *Config) {}, fakeSecrets{"jira_email": emailValue}},
		{"email secret missing", func(_ *Config) {}, fakeSecrets{"jira_api_token": tokenValue}},
	}
	for _, tc := range cases {
		c := good
		tc.mutate(&c)
		_, err := New(c, tc.secrets, &clock{at: now}, (&waits{}).Wait, nil)
		require.Error(t, err, tc.name)
		require.NotContains(t, err.Error(), tokenValue, tc.name)
		require.NotContains(t, err.Error(), emailValue, tc.name)
	}
	_, err := New(good, secrets, &clock{at: now}, nil, nil)
	require.Error(t, err, "a waiter is required")
}

func TestATransportFailureIsUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	base := srv.URL
	srv.Close()
	cl, err := New(Config{BaseURL: base, Project: "KAN", EmailSecret: "jira_email", TokenSecret: "jira_api_token"},
		fakeSecrets{"jira_email": emailValue, "jira_api_token": tokenValue}, &clock{at: now}, (&waits{}).Wait, nil)
	require.NoError(t, err)

	_, _, err = cl.Changes(context.Background(), "")

	require.ErrorIs(t, err, ports.ErrUnavailable)
}

func TestAFailedPollDoesNotAdvanceTheCursor(t *testing.T) {
	f := newFake(t, script(cat(
		rep(unavailable, 3),
		[]scripted{{status: http.StatusOK, body: search(true, "",
			issue("KAN-1", ana, history("10042", "2026-11-04T09:55:00.000+0000", "status")))}},
	)...))
	cl := newClient(t, f, &clock{at: now})
	since := poll.Format(now.Add(-time.Hour))

	changes, next, err := cl.Changes(context.Background(), since)

	require.ErrorIs(t, err, ports.ErrUnavailable)
	require.Empty(t, changes)
	require.Empty(t, next, "a failed poll advances nothing; the caller keeps its last good cursor")

	changes, next, err = cl.Changes(context.Background(), since)

	require.NoError(t, err)
	require.Len(t, changes, 1, "the change is still reported after the failed poll")
	require.Equal(t, poll.Format(now), next)
	reqs := f.requests()
	require.Len(t, reqs, 4, "three attempts of the failed poll, then one good poll")
	require.Equal(t, reqs[0].Query.Get("jql"), reqs[3].Query.Get("jql"), "the retry reads the same window")
	require.Contains(t, reqs[3].Query.Get("jql"), `AFTER "2026-11-04 08:50"`)
}

func TestAFailedPollRecordsNothingSoTheNextPollReportsEveryChange(t *testing.T) {
	f := newFake(t, script(
		scripted{status: http.StatusOK, body: search(true, "",
			issue("KAN-1", ana, history("10042", "2026-11-04T09:55:00.000+0000", "status")),
			issue("KAN-2", ana, history("10043", "not a time", "status")))},
		scripted{status: http.StatusOK, body: search(true, "",
			issue("KAN-1", ana, history("10042", "2026-11-04T09:55:00.000+0000", "status")))},
	))
	cl := newClient(t, f, &clock{at: now})
	since := poll.Format(now.Add(-time.Hour))

	_, _, err := cl.Changes(context.Background(), since)
	require.Error(t, err, "KAN-2 has an unreadable time, so the whole poll fails")

	changes, _, err := cl.Changes(context.Background(), since)

	require.NoError(t, err)
	require.Len(t, changes, 1, "KAN-1 was in the failed poll; it must still be reported, not dropped as seen")
	require.Equal(t, id.ChangeID("KAN-1"), changes[0].Key)
}
