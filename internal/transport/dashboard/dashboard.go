// Package dashboard is the read-only web edge of the dashboard (TECH_SPEC 14.2, 14.3, D53). It serves plain
// HTML pages, one server-sent event stream per change, and two embedded static files. It only reads: it calls
// app/dashboard and never writes to the log.
//
// # Routes
//
//   - GET /: the list of changes.
//   - GET /changes/{id}: one change, with its gate, findings, reviewers, open questions and timeline.
//   - GET /changes/{id}/report: the review report page, which says why the change can merge or not.
//   - GET /changes/{id}/report.json: the json-render spec of that page, built from the log with no model.
//   - GET /sse/changes/{id}: a server-sent event per new log event. The stream starts after the version named by
//     the after query parameter or the Last-Event-ID header, whichever is higher. Without either it starts at
//     version 1.
//   - GET /assets/{name}: the embedded dashboard.css and dashboard.js, and report.css and report.js, which
//     web/review-report builds.
//
// # Invariants
//
//   - The page never runs HTML or script from the log. Every value is escaped by html/template, and the
//     script only sets text content.
//   - Every dashboard response carries a content policy that allows script and style from this origin only.
//   - Mount hands every path outside the dashboard to the handler it wraps, so other routes keep their answers.
//
// # Failure modes
//
//   - An unknown change or an invalid change id is 404. A version that is not a number is 400.
//   - A failed read is logged and answered with 500 and a fixed text, so no database detail reaches the client.
//   - A failed read during a live stream is logged, and the stream waits for the next poll.
package dashboard

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	appdashboard "github.com/prathameshk421/flip_the_switch_hackathon/internal/app/dashboard"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

// assetFiles holds the static files the pages use. Only files in assets/ are served.
//
//go:embed assets
var assetFiles embed.FS

const (
	// defaultPollEvery is how often a live stream reads the log when Options does not say.
	defaultPollEvery = 2 * time.Second
	// keepAliveEvery is how often a live stream sends a comment, so proxies keep the connection open.
	keepAliveEvery = 15 * time.Second
	// contentPolicy lets a page load script and style from this origin only. The pages hold no inline code.
	contentPolicy = "default-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self'; " +
		"base-uri 'none'; form-action 'none'; frame-ancestors 'none'"
)

// staticFile maps the name of a static file to its embedded path and content type. Only the names listed here are
// served; any other name is 404. The request value is only compared, never joined into a path.
func staticFile(name string) (file, contentType string, ok bool) {
	switch name {
	case "dashboard.css":
		return "assets/dashboard.css", "text/css; charset=utf-8", true
	case "dashboard.js":
		return "assets/dashboard.js", "text/javascript; charset=utf-8", true
	case "report.css":
		return "assets/report.css", "text/css; charset=utf-8", true
	case "report.js":
		return "assets/report.js", "text/javascript; charset=utf-8", true
	}
	return "", "", false
}

// Options tunes the edge. The server supplies the ticker; the package never reads the system clock (R5).
type Options struct {
	// PollEvery is how often a live stream reads the log for new events. Zero takes the default. It must not be
	// negative.
	PollEvery time.Duration
	// NewTicker starts a ticker that sends on ticks every every until stop is called. It is required.
	NewTicker func(every time.Duration) (ticks <-chan time.Time, stop func())
}

// Mount returns next with the dashboard routes in front of it. Requests for the dashboard paths are served here,
// and every other request goes to next unchanged. It refuses a nil history, a nil logger, a nil next, a nil
// NewTicker, and a negative poll interval.
func Mount(next http.Handler, history appdashboard.History, logger *slog.Logger, opts Options) (http.Handler, error) {
	if next == nil {
		return nil, errors.New("dashboard next handler must not be nil")
	}
	if logger == nil {
		return nil, errors.New("dashboard logger must not be nil")
	}
	if opts.NewTicker == nil {
		return nil, errors.New("dashboard ticker must not be nil")
	}
	if opts.PollEvery < 0 {
		return nil, errors.New("dashboard poll interval must not be negative")
	}
	svc, err := appdashboard.New(history)
	if err != nil {
		return nil, fmt.Errorf("dashboard: %w", err)
	}
	listTmpl, err := template.New("list").Parse(listHTML)
	if err != nil {
		return nil, fmt.Errorf("dashboard list page: %w", err)
	}
	detailTmpl, err := template.New("detail").Parse(detailHTML)
	if err != nil {
		return nil, fmt.Errorf("dashboard detail page: %w", err)
	}
	every := opts.PollEvery
	if every == 0 {
		every = defaultPollEvery
	}
	e := &edge{svc: svc, logger: logger, every: every, newTicker: opts.NewTicker, list: listTmpl, detail: detailTmpl}

	routes := http.NewServeMux()
	routes.HandleFunc("GET /{$}", e.listChanges)
	routes.HandleFunc("GET /changes/{id}", e.changeDetail)
	routes.HandleFunc("GET /changes/{id}/report", e.reportPage)
	routes.HandleFunc("GET /changes/{id}/report.json", e.reportSpec)
	routes.HandleFunc("GET /sse/changes/{id}", e.stream)
	routes.HandleFunc("GET /assets/{name}", e.asset)
	return &mounted{routes: routes, next: next}, nil
}

// mounted sends the dashboard paths to its routes and every other path to next.
type mounted struct {
	routes http.Handler
	next   http.Handler
}

func (m *mounted) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !owns(r.URL.Path) {
		m.next.ServeHTTP(w, r)
		return
	}
	w.Header().Set("Content-Security-Policy", contentPolicy)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	m.routes.ServeHTTP(w, r)
}

// owns reports whether the dashboard serves path.
func owns(p string) bool {
	return p == "/" || strings.HasPrefix(p, "/changes/") || strings.HasPrefix(p, "/sse/changes/") ||
		strings.HasPrefix(p, "/assets/")
}

// edge holds what the handlers need. It keeps no state between requests.
type edge struct {
	svc       *appdashboard.Service
	logger    *slog.Logger
	every     time.Duration
	newTicker func(every time.Duration) (<-chan time.Time, func())
	list      *template.Template
	detail    *template.Template
}

// listPage is the data of the list page.
type listPage struct {
	Rows []appdashboard.ChangeSummary
}

// detailView is the data of the change page. Times are text, already in UTC.
type detailView struct {
	D        appdashboard.Detail
	Stream   string
	Updated  string
	Timeline []rowView
	Asks     []askView
}

// rowView is one timeline row of the change page.
type rowView struct {
	Version int64
	At      string
	Type    string
	Actor   string
	Note    string
}

// askView is one open question of the change page.
type askView struct {
	ID    string
	Since string
}

// streamItem is the data of one live event. The page reads it as JSON.
type streamItem struct {
	Version int64  `json:"version"`
	At      string `json:"at"`
	Type    string `json:"type"`
	Actor   string `json:"actor"`
	Note    string `json:"note"`
	State   string `json:"state"`
	Gate    string `json:"gate"`
}

func (e *edge) listChanges(w http.ResponseWriter, r *http.Request) {
	rows, err := e.svc.Changes(r.Context())
	if err != nil {
		e.fail(w, r, "list changes", err)
		return
	}
	e.render(w, r, e.list, listPage{Rows: rows})
}

func (e *edge) changeDetail(w http.ResponseWriter, r *http.Request) {
	cid, ok := changeID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	d, found, err := e.svc.Detail(r.Context(), cid)
	if err != nil {
		e.fail(w, r, "read change", err)
		return
	}
	if !found {
		http.NotFound(w, r)
		return
	}
	e.render(w, r, e.detail, newDetailView(d))
}

// reportPage serves the empty page that the report script fills. It reads the change first, so an unknown change is 404.
func (e *edge) reportPage(w http.ResponseWriter, r *http.Request) {
	if _, ok := e.report(w, r); !ok {
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// A failed write means the client has gone away; there is no one left to tell.
	_, _ = io.WriteString(w, reportHTML)
}

// reportSpec serves the json-render spec of the review report.
func (e *edge) reportSpec(w http.ResponseWriter, r *http.Request) {
	spec, ok := e.report(w, r)
	if !ok {
		return
	}
	raw, err := appdashboard.MarshalReport(spec)
	if err != nil {
		e.fail(w, r, "encode report", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeContent(w, r, "report.json", time.Time{}, bytes.NewReader(raw))
}

// report reads the report of the requested change, or answers the request itself.
func (e *edge) report(w http.ResponseWriter, r *http.Request) (appdashboard.Spec, bool) {
	cid, ok := changeID(r)
	if !ok {
		http.NotFound(w, r)
		return appdashboard.Spec{}, false
	}
	spec, found, err := e.svc.Report(r.Context(), cid)
	if err != nil {
		e.fail(w, r, "read report", err)
		return appdashboard.Spec{}, false
	}
	if !found {
		http.NotFound(w, r)
		return appdashboard.Spec{}, false
	}
	return spec, true
}

// stream sends one event for each log event after the resume version, and then one for each new event until the
// client goes away or the change is no longer in the log.
func (e *edge) stream(w http.ResponseWriter, r *http.Request) {
	cid, ok := changeID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	sent, err := resumeVersion(r)
	if err != nil {
		http.Error(w, "version must be 0 or more", http.StatusBadRequest)
		return
	}
	d, found, err := e.svc.Detail(r.Context(), cid)
	if err != nil {
		e.fail(w, r, "start live stream", err)
		return
	}
	if !found {
		http.NotFound(w, r)
		return
	}

	rc, err := startStream(w)
	if err != nil {
		e.logger.WarnContext(r.Context(), "dashboard stream could not start", "error", err)
		return
	}
	if sent, err = writeNew(w, rc, d, sent); err != nil {
		return
	}
	e.follow(r.Context(), w, rc, cid, sent)
}

// startStream sets the event stream headers, sends them, and returns the controller that flushes each event.
func startStream(w http.ResponseWriter) (*http.ResponseController, error) {
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	rc := http.NewResponseController(w)
	return rc, rc.Flush()
}

// follow reads the log on each poll and writes new events, and sends a keep-alive comment on each beat. It returns
// when the client goes away or the change leaves the log.
func (e *edge) follow(ctx context.Context, w io.Writer, rc *http.ResponseController, cid id.ChangeID, sent int64) {
	polls, stopPoll := e.newTicker(e.every)
	defer stopPoll()
	beats, stopBeat := e.newTicker(keepAliveEvery)
	defer stopBeat()
	for {
		select {
		case <-ctx.Done():
			return
		case <-polls:
			d, found, err := e.svc.Detail(ctx, cid)
			if err != nil {
				e.logger.WarnContext(ctx, "dashboard stream read failed", "error", err)
				continue
			}
			if !found {
				return
			}
			if sent, err = writeNew(w, rc, d, sent); err != nil {
				return
			}
		case <-beats:
			if _, err := io.WriteString(w, ": keep-alive\n\n"); err != nil {
				return
			}
			if err := rc.Flush(); err != nil {
				return
			}
		}
	}
}

// asset serves one embedded static file. A name with another extension, or a missing one, is 404.
func (e *edge) asset(w http.ResponseWriter, r *http.Request) {
	file, ct, ok := staticFile(r.PathValue("name"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "no-cache")
	// The file is static and embedded, so no escaping applies: escaping would break the script and style it serves.
	http.ServeFileFS(w, r, assetFiles, file)
}

// render runs a page template into a buffer first, so a template error never leaves a half page.
func (e *edge) render(w http.ResponseWriter, r *http.Request, t *template.Template, data any) {
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		e.fail(w, r, "render "+t.Name(), err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// A failed write means the client has gone away; there is no one left to tell.
	_, _ = buf.WriteTo(w)
}

// fail logs the error and answers with a fixed text. The client never sees the database error.
func (e *edge) fail(w http.ResponseWriter, r *http.Request, what string, err error) {
	e.logger.ErrorContext(r.Context(), "dashboard "+what+" failed", "error", err)
	http.Error(w, "dashboard unavailable", http.StatusInternalServerError)
}

// writeNew writes one event for each timeline item after sent and returns the version of the last one written.
// It flushes at the end, so the client gets each poll at once.
func writeNew(w io.Writer, rc *http.ResponseController, d appdashboard.Detail, sent int64) (int64, error) {
	for _, item := range d.Timeline {
		if item.Version <= sent {
			continue
		}
		data, err := json.Marshal(streamItem{
			Version: item.Version,
			At:      stamp(item.At),
			Type:    item.Type,
			Actor:   item.Actor,
			Note:    item.Note,
			State:   d.State,
			Gate:    d.Gate.Status,
		})
		if err != nil {
			return sent, fmt.Errorf("encode event %d: %w", item.Version, err)
		}
		if _, err := fmt.Fprintf(w, "id: %d\nevent: change\ndata: %s\n\n", item.Version, data); err != nil {
			return sent, err
		}
		sent = item.Version
	}
	return sent, rc.Flush()
}

// resumeVersion is the version a stream starts after. The after query parameter and the Last-Event-ID header
// both name one, and the higher one wins, so a reconnect never repeats an event.
func resumeVersion(r *http.Request) (int64, error) {
	after, err := parseVersion(r.URL.Query().Get("after"))
	if err != nil {
		return 0, err
	}
	last, err := parseVersion(r.Header.Get("Last-Event-ID"))
	if err != nil {
		return 0, err
	}
	return max(after, last), nil
}

// parseVersion reads a version. An empty value is 0.
func parseVersion(s string) (int64, error) {
	if s == "" {
		return 0, nil
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil || v < 0 {
		return 0, fmt.Errorf("version %q must be 0 or more", s)
	}
	return v, nil
}

// changeID reads the change id of the path. It is false when the id is not a change id.
func changeID(r *http.Request) (id.ChangeID, bool) {
	cid, err := id.ParseChangeID(r.PathValue("id"))
	return cid, err == nil
}

// newDetailView turns a read model into the data of the change page.
func newDetailView(d appdashboard.Detail) detailView {
	v := detailView{D: d, Stream: "/sse/changes/" + string(d.ID), Updated: stamp(d.UpdatedAt)}
	for _, item := range d.Timeline {
		v.Timeline = append(v.Timeline, rowView{
			Version: item.Version,
			At:      stamp(item.At),
			Type:    item.Type,
			Actor:   item.Actor,
			Note:    item.Note,
		})
	}
	for _, a := range d.OpenAsks {
		v.Asks = append(v.Asks, askView{ID: a.ID, Since: stamp(a.Since)})
	}
	return v
}

// stamp is a time as the pages show it: UTC, to the second.
func stamp(t time.Time) string {
	return t.UTC().Format("2006-01-02 15:04:05 UTC")
}
