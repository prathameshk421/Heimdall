package jira

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/tracker/internal/criteria"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/tracker/internal/poll"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

const (
	searchPath     = "/rest/api/3/search/jql"
	issuePath      = "/rest/api/3/issue/"
	requestTimeout = 10 * time.Second
	maxBody        = 8 << 20
	maxPages       = 100
	pageSize       = "50"
	// jqlTime is the minute resolution JQL compares dates at (V55).
	jqlTime = "2006-01-02 15:04"
	// createdTime is how Jira writes a changelog timestamp, for example 2026-11-04T09:55:00.000+0000.
	createdTime = "2006-01-02T15:04:05.999999999-0700"
	// ticketFields names the fields Ticket reads.
	ticketFields = "summary,status,assignee,description"
)

var (
	reTicketKey = regexp.MustCompile(`^[A-Z][A-Z0-9_]*-[0-9]+$`)
	reProject   = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
)

// Config names the Jira site, the tracker project and the secrets that hold the account email and API token. The
// secrets are read by name through Secrets, so the values never sit in this struct (TECH_SPEC 2.6).
type Config struct {
	BaseURL     string
	Project     string
	EmailSecret string
	TokenSecret string
}

// Secrets reads one secret by name. config.Config satisfies it.
type Secrets interface {
	Secret(name string) (string, error)
}

// Client polls Jira and reads tickets. It is safe for concurrent use.
type Client struct {
	base    string
	project string
	auth    string
	clock   ports.Clock
	hc      *http.Client
	seen    poll.Seen
}

var _ ports.TicketReader = (*Client)(nil)

// New validates the config and reads the two secrets. The base URL must be https, or http to a loopback address
// for a local test server, so the token never travels in clear to another host.
func New(cfg Config, secrets Secrets, clock ports.Clock, hc *http.Client) (*Client, error) {
	base, err := checkBaseURL(cfg.BaseURL)
	if err != nil {
		return nil, err
	}
	if !reProject.MatchString(cfg.Project) {
		return nil, fmt.Errorf("jira: project %q must be an upper-case Jira project key", cfg.Project)
	}
	email, err := readSecret(secrets, cfg.EmailSecret)
	if err != nil {
		return nil, err
	}
	token, err := readSecret(secrets, cfg.TokenSecret)
	if err != nil {
		return nil, err
	}
	if hc == nil {
		hc = &http.Client{}
	}
	return &Client{
		base:    base,
		project: cfg.Project,
		auth:    "Basic " + base64.StdEncoding.EncodeToString([]byte(email+":"+token)),
		clock:   clock,
		hc:      hc,
	}, nil
}

// checkBaseURL accepts an absolute https URL, or http to a loopback host.
func checkBaseURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("jira: base url %q must be an absolute URL", raw)
	}
	switch {
	case u.Scheme == "https":
	case u.Scheme == "http" && isLoopback(u.Hostname()):
	default:
		return "", fmt.Errorf("jira: base url %q must use https (http only for a loopback test server)", raw)
	}
	return strings.TrimSuffix(raw, "/"), nil
}

// isLoopback reports whether host is localhost or a loopback IP.
func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// readSecret returns one non-empty secret. The error names the secret, never its value.
func readSecret(secrets Secrets, name string) (string, error) {
	if name == "" {
		return "", errors.New("jira: a secret name is required")
	}
	if secrets == nil {
		return "", fmt.Errorf("jira: no secret reader for %s", name)
	}
	v, err := secrets.Secret(name)
	if err != nil {
		return "", fmt.Errorf("jira: read secret %s: %w", name, err)
	}
	if v == "" {
		return "", fmt.Errorf("jira: secret %s is empty", name)
	}
	return v, nil
}

// Changes returns the changelog entries of human-assigned tickets that moved to In Progress, from cursor minus the
// overlap. Each entry is keyed by ticket key and changelog id; a repeat is dropped. The new cursor is this poll's
// clock time.
func (c *Client) Changes(ctx context.Context, since ports.Cursor) ([]ports.TicketChange, ports.Cursor, error) {
	now := c.clock.Now()
	start, err := poll.WindowStart(since, now)
	if err != nil {
		return nil, "", fmt.Errorf("jira changes: %w", err)
	}
	jql := fmt.Sprintf(`project = %s AND assignee IS NOT EMPTY AND status CHANGED TO "In Progress" AFTER "%s"`,
		c.project, start.UTC().Format(jqlTime))
	issues, err := c.searchAll(ctx, jql)
	if err != nil {
		return nil, "", err
	}
	var out []ports.TicketChange
	for _, is := range issues {
		changes, err := c.issueChanges(is, start)
		if err != nil {
			return nil, "", err
		}
		out = append(out, changes...)
	}
	c.seen.Forget(start)
	return out, poll.Format(now), nil
}

// issueChanges returns the new changelog entries of one issue inside the window. Apps and unassigned issues are
// skipped (the JQL already asks for an assignee, and an app is not a person).
func (c *Client) issueChanges(is searchIssue, start time.Time) ([]ports.TicketChange, error) {
	if is.Fields.Assignee == nil || is.Fields.Assignee.AccountType == "app" {
		return nil, nil
	}
	if !reTicketKey.MatchString(is.Key) {
		return nil, fmt.Errorf("jira changes: issue key %q is not a ticket key", is.Key)
	}
	key := id.ChangeID(is.Key)
	var out []ports.TicketChange
	for _, h := range is.Changelog.Histories {
		at, err := time.Parse(createdTime, h.Created)
		if err != nil {
			return nil, fmt.Errorf("jira changes: %s history %s time: %w", is.Key, h.ID, err)
		}
		if at.Before(start) {
			continue
		}
		fields := fieldNames(h.Items)
		if len(fields) == 0 || !c.seen.Observe(key, h.ID, at) {
			continue
		}
		out = append(out, ports.TicketChange{Key: key, ChangelogID: h.ID, Fields: fields, At: at.UTC()})
	}
	return out, nil
}

// fieldNames returns the distinct field names a changelog entry touched, in order.
func fieldNames(items []changeItem) []string {
	var names []string
	seen := map[string]bool{}
	for _, it := range items {
		if it.Field != "" && !seen[it.Field] {
			seen[it.Field] = true
			names = append(names, it.Field)
		}
	}
	return names
}

// searchAll follows the search pages to the end, up to maxPages.
func (c *Client) searchAll(ctx context.Context, jql string) ([]searchIssue, error) {
	var all []searchIssue
	token := ""
	for range maxPages {
		q := url.Values{}
		q.Set("jql", jql)
		q.Set("fields", "assignee")
		q.Set("expand", "changelog")
		q.Set("maxResults", pageSize)
		if token != "" {
			q.Set("nextPageToken", token)
		}
		var page searchResponse
		if err := c.getJSON(ctx, searchPath, q, &page); err != nil {
			return nil, fmt.Errorf("jira changes: %w", err)
		}
		all = append(all, page.Issues...)
		if page.IsLast || page.NextPageToken == "" {
			return all, nil
		}
		token = page.NextPageToken
	}
	return nil, fmt.Errorf("jira changes: more than %d search pages", maxPages)
}

// Ticket returns one ticket with its acceptance criteria. The key is checked before any request is made.
func (c *Client) Ticket(ctx context.Context, key id.ChangeID) (ports.Ticket, error) {
	if !reTicketKey.MatchString(string(key)) {
		return ports.Ticket{}, fmt.Errorf("jira ticket: %q is not a ticket key", string(key))
	}
	var is ticketResponse
	if err := c.getJSON(ctx, issuePath+string(key), url.Values{"fields": {ticketFields}}, &is); err != nil {
		return ports.Ticket{}, fmt.Errorf("jira ticket %s: %w", string(key), err)
	}
	desc, err := descriptionText(is.Fields.Description)
	if err != nil {
		return ports.Ticket{}, fmt.Errorf("jira ticket %s: %w", string(key), err)
	}
	cs, err := criteria.Parse(desc)
	if err != nil {
		return ports.Ticket{}, fmt.Errorf("jira ticket %s: %w", string(key), err)
	}
	tk := ports.Ticket{
		Key:      key,
		Title:    is.Fields.Summary,
		Status:   is.Fields.Status.Name,
		Criteria: cs,
	}
	if is.Fields.Assignee != nil {
		tk.Assignee = change.ActorID(is.Fields.Assignee.AccountID)
	}
	if err := tk.Validate(); err != nil {
		return ports.Ticket{}, fmt.Errorf("jira ticket %s: %w", string(key), err)
	}
	return tk, nil
}

// getJSON sends one authenticated GET with a deadline and decodes the reply. The token is only ever in the header.
func (c *Client) getJSON(ctx context.Context, path string, query url.Values, out any) error {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	target := c.base + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return fmt.Errorf("build request %s: %w", path, err)
	}
	req.Header.Set("Authorization", c.auth)
	req.Header.Set("Accept", "application/json")
	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("%s: %w: %w", path, ports.ErrUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if err := statusError(resp.StatusCode, path); err != nil {
		return err
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return fmt.Errorf("%s: read body: %w: %w", path, ports.ErrUnavailable, err)
	}
	if len(raw) > maxBody {
		return fmt.Errorf("%s: response larger than %d bytes", path, maxBody)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("%s: decode reply: %w", path, err)
	}
	return nil
}

// statusError maps an HTTP status to a port error. The body is never included, since it may echo user text.
func statusError(status int, path string) error {
	switch {
	case status >= 200 && status < 300:
		return nil
	case status == http.StatusUnauthorized, status == http.StatusForbidden:
		return fmt.Errorf("%s: status %d: %w", path, status, ports.ErrUnauthorized)
	case status == http.StatusNotFound:
		return fmt.Errorf("%s: status %d: %w", path, status, ports.ErrNotFound)
	case status == http.StatusTooManyRequests, status >= 500:
		return fmt.Errorf("%s: status %d: %w", path, status, ports.ErrUnavailable)
	default:
		return fmt.Errorf("%s: unexpected status %d", path, status)
	}
}

// searchResponse is one page of the JQL search.
type searchResponse struct {
	Issues        []searchIssue `json:"issues"`
	NextPageToken string        `json:"nextPageToken"`
	IsLast        bool          `json:"isLast"`
}

// searchIssue is one issue of a search page with its assignee and changelog.
type searchIssue struct {
	Key    string `json:"key"`
	Fields struct {
		Assignee *person `json:"assignee"`
	} `json:"fields"`
	Changelog struct {
		Histories []changelogEntry `json:"histories"`
	} `json:"changelog"`
}

// person is a Jira account. AccountType "app" marks an automation, not a human.
type person struct {
	AccountID   string `json:"accountId"`
	AccountType string `json:"accountType"`
}

// changelogEntry is one changelog entry: its id (the dedupe key with the ticket key) and when it was written.
type changelogEntry struct {
	ID      string       `json:"id"`
	Created string       `json:"created"`
	Items   []changeItem `json:"items"`
}

// changeItem names one field a changelog entry touched.
type changeItem struct {
	Field string `json:"field"`
}

// ticketResponse is the issue read by Ticket.
type ticketResponse struct {
	Fields struct {
		Summary string `json:"summary"`
		Status  struct {
			Name string `json:"name"`
		} `json:"status"`
		Assignee    *person         `json:"assignee"`
		Description json.RawMessage `json:"description"`
	} `json:"fields"`
}
