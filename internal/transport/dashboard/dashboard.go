// Package dashboard is the read-only view of the changes: the state of each one, its last event, and what blocks its
// merge (A7). It reads the event log only and never writes. The page is server-rendered; the one script it carries
// subscribes to the SSE stream and reloads the page when a change moves. Nothing else is loaded from outside.
package dashboard

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net"
	"net/http"
	"sort"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/clock"
)

//go:embed page.html
var pageFS embed.FS

// parsePage parses the embedded page once, when the handler is built.
func parsePage() (*template.Template, error) {
	return template.ParseFS(pageFS, "page.html")
}

// Source is the read side of the event log.
type Source interface {
	ListChanges(ctx context.Context) ([]id.ChangeID, error)
	Load(ctx context.Context, changeID id.ChangeID) ([]change.Event, error)
}

// Row is one change as the page shows it.
type Row struct {
	ID       string
	State    string
	LastType string
	Version  int64
	Blocked  bool
	Reasons  []string
}

// Handler serves the page at / and the SSE stream at /events. Only a loopback caller is served (the same rule as the
// server's listener, so the dashboard adds no ingress).
type Handler struct {
	src      Source
	logger   *slog.Logger
	interval time.Duration
	page     *template.Template
}

// New returns the dashboard handler. interval is how often the stream looks at the log.
func New(src Source, logger *slog.Logger, interval time.Duration) (*Handler, error) {
	if src == nil || logger == nil {
		return nil, errors.New("dashboard needs a source and a logger")
	}
	if interval <= 0 {
		return nil, errors.New("dashboard interval must be positive")
	}
	page, err := parsePage()
	if err != nil {
		return nil, fmt.Errorf("parse dashboard page: %w", err)
	}
	return &Handler{src: src, logger: logger, interval: interval, page: page}, nil
}

// ServeHTTP routes the two paths and refuses anything that is not a loopback request.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !loopback(r.RemoteAddr) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	switch r.URL.Path {
	case "/":
		h.index(w, r)
	case "/events":
		h.stream(w, r)
	default:
		http.NotFound(w, r)
	}
}

func loopback(remote string) bool {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		host = remote
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (h *Handler) index(w http.ResponseWriter, r *http.Request) {
	rows, err := h.rows(r.Context())
	if err != nil {
		h.logger.ErrorContext(r.Context(), "dashboard could not read the log", "error", err)
		http.Error(w, "the dashboard could not read the change log", http.StatusInternalServerError)
		return
	}
	var buf bytes.Buffer
	if err := h.page.Execute(&buf, struct{ Rows []Row }{rows}); err != nil {
		http.Error(w, "render failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'")
	_, _ = w.Write(buf.Bytes())
}

// rows reads every change and builds its row. A change whose log does not replay still shows, with its last event.
func (h *Handler) rows(ctx context.Context) ([]Row, error) {
	ids, err := h.src.ListChanges(ctx)
	if err != nil {
		return nil, fmt.Errorf("list changes: %w", err)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	rows := make([]Row, 0, len(ids))
	for _, cid := range ids {
		evs, err := h.src.Load(ctx, cid)
		if err != nil {
			return nil, fmt.Errorf("load %s: %w", cid, err)
		}
		rows = append(rows, rowOf(cid, evs))
	}
	return rows, nil
}

func rowOf(cid id.ChangeID, evs []change.Event) Row {
	row := Row{ID: string(cid), State: "unreadable"}
	if state, err := change.Replay(evs); err == nil {
		row.State = string(state)
	}
	if len(evs) > 0 {
		last := evs[len(evs)-1]
		row.LastType = string(last.Type)
		row.Version = last.Version
	}
	for i := len(evs) - 1; i >= 0; i-- {
		if evs[i].Type != change.EventGateEvaluated {
			continue
		}
		// Only the verdict and its reasons are read here. The full payload carries SHAs that the strict decoder checks, so
		// a view that reads it can fail on a valid gate result.
		var g struct {
			OK      bool     `json:"ok"`
			Reasons []string `json:"reasons"`
		}
		if err := json.Unmarshal(evs[i].Payload, &g); err == nil && !g.OK {
			row.Blocked = true
			row.Reasons = g.Reasons
		}
		break
	}
	return row
}

// stream sends one "change" event for each change whose version moved since the last look. The first look sends
// every change, so a page that connects late catches up.
func (h *Handler) stream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming is not supported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	seen := map[string]int64{}
	for {
		ids, err := h.src.ListChanges(r.Context())
		if err != nil {
			h.logger.ErrorContext(r.Context(), "dashboard stream could not list changes", "error", err)
		}
		for _, cid := range ids {
			evs, err := h.src.Load(r.Context(), cid)
			if err != nil || len(evs) == 0 {
				continue
			}
			last := evs[len(evs)-1].Version
			if seen[string(cid)] == last {
				continue
			}
			seen[string(cid)] = last
			if _, err := fmt.Fprintf(w, "event: change\ndata: %s\n\n", cid); err != nil {
				return
			}
			flusher.Flush()
		}
		if err := clock.Wait(r.Context(), h.interval); err != nil {
			return
		}
	}
}
