// Package dashboard is the read model behind the read-only web view (TECH_SPEC 14.3, D53). It folds the event log
// of each change into plain values for the web edge to show. It only reads: it never appends, decides or calls a
// tracker or a chat.
//
// Each read loads the whole log of a change and folds it in one pass. The state comes from change.Replay. The
// findings, reviewers, open questions and gate come from the payloads, so they still show when the state machine
// refuses the log: the timeline is the record, and the state is the verdict on it.
package dashboard

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

// Gate statuses as the dashboard shows them.
const (
	GateNotEvaluated = "not evaluated"
	GatePassed       = "passed"
	GateBlocked      = "blocked"
)

// Reviewer statuses as the dashboard shows them.
const (
	ReviewerWorking     = "working"
	ReviewerSubmitted   = "submitted"
	ReviewerUnavailable = "unavailable"
	ReviewerWaived      = "waived"
)

// unknownState is the state of a log that the reducer refuses.
const unknownState = "unknown"

// unreadable is the timeline note of an event whose payload does not decode.
const unreadable = "payload not readable"

// History is the read side of the event log. The Postgres store satisfies it.
type History interface {
	ChangeIDs(ctx context.Context) ([]id.ChangeID, error)
	Load(ctx context.Context, changeID id.ChangeID) ([]change.Event, error)
}

// Service builds the read model from a History.
type Service struct {
	history History
}

// New returns a Service. It refuses a nil history.
func New(history History) (*Service, error) {
	if history == nil {
		return nil, errors.New("dashboard history must not be nil")
	}
	return &Service{history: history}, nil
}

// ChangeSummary is one row of the change list.
type ChangeSummary struct {
	ID           id.ChangeID
	Ticket       string
	State        string
	Version      int64
	Gate         string
	OpenFindings int
}

// Detail is everything the change page shows. It is rebuilt from the whole log on each read.
type Detail struct {
	ID        id.ChangeID
	Ticket    string
	Repo      string
	State     string
	Problem   string
	Version   int64
	UpdatedAt time.Time
	Blockers  []string
	Gate      Gate
	Findings  []Finding
	Reviewers []Reviewer
	OpenAsks  []Ask
	Timeline  []TimelineItem
}

// Gate is the latest gate decision in the log.
type Gate struct {
	Status      string
	Reasons     []string
	SHA         string
	EvaluatedAt time.Time
}

// Finding is the latest record of one finding.
type Finding struct {
	Ref   string
	Tier  string
	State string
	By    string
}

// Reviewer is the latest status of one reviewer.
type Reviewer struct {
	Name   string
	Status string
}

// Ask is a question that has no answer yet.
type Ask struct {
	ID    string
	Since time.Time
}

// TimelineItem is one event of the log, in plain words.
type TimelineItem struct {
	Version int64
	At      time.Time
	Type    string
	Actor   string
	Note    string
}

// Changes lists every change in the log with its state, gate and open findings.
func (s *Service) Changes(ctx context.Context) ([]ChangeSummary, error) {
	ids, err := s.history.ChangeIDs(ctx)
	if err != nil {
		return nil, fmt.Errorf("list changes: %w", err)
	}
	rows := make([]ChangeSummary, 0, len(ids))
	for _, cid := range ids {
		d, found, err := s.Detail(ctx, cid)
		if err != nil {
			return nil, err
		}
		if found {
			rows = append(rows, d.summary())
		}
	}
	return rows, nil
}

// Detail returns the read model of one change. found is false when the log has no events for it.
func (s *Service) Detail(ctx context.Context, changeID id.ChangeID) (Detail, bool, error) {
	evs, err := s.history.Load(ctx, changeID)
	if err != nil {
		return Detail{}, false, fmt.Errorf("load %s: %w", changeID, err)
	}
	if len(evs) == 0 {
		return Detail{}, false, nil
	}
	return fold(changeID, evs), true, nil
}

// summary is the row of d in the change list.
func (d Detail) summary() ChangeSummary {
	open := 0
	for _, f := range d.Findings {
		if !finding.State(f.State).Terminal() {
			open++
		}
	}
	return ChangeSummary{
		ID:           d.ID,
		Ticket:       d.Ticket,
		State:        d.State,
		Version:      d.Version,
		Gate:         d.Gate.Status,
		OpenFindings: open,
	}
}

// folder collects the read model of one log. It keeps the latest record of each finding and reviewer.
type folder struct {
	d         Detail
	records   map[id.FindingID]change.FindingRecord
	reviewers map[string]string
	asks      []Ask
}

// fold builds the read model of a non-empty log, in version order.
func fold(cid id.ChangeID, evs []change.Event) Detail {
	f := &folder{
		records:   map[id.FindingID]change.FindingRecord{},
		reviewers: map[string]string{},
	}
	f.d.ID = cid
	f.d.Ticket = string(cid)
	f.d.Gate = Gate{Status: GateNotEvaluated}
	for _, e := range evs {
		f.d.Timeline = append(f.d.Timeline, f.item(e))
	}
	last := evs[len(evs)-1]
	f.d.Version = last.Version
	f.d.UpdatedAt = last.OccurredAt

	state, err := change.Replay(evs)
	if err != nil {
		f.d.State = unknownState
		f.d.Problem = err.Error()
	} else {
		f.d.State = string(state)
	}
	return f.result()
}

// item is the timeline row of e. It also folds the payload of e into the read model.
func (f *folder) item(e change.Event) TimelineItem {
	item := TimelineItem{Version: e.Version, At: e.OccurredAt, Type: string(e.Type), Actor: string(e.Actor.ID)}
	p, err := change.Decode(e)
	if err != nil {
		item.Note = unreadable
		return item
	}
	item.Note = f.apply(e, p)
	return item
}

// apply folds one readable payload and returns its note in plain words.
func (f *folder) apply(e change.Event, p change.Payload) string {
	switch v := p.(type) {
	case change.Triggered:
		return f.triggered(v)
	case change.ReviewersPicked, change.ReviewSubmitted, change.ReviewUnavailable, change.ReviewWaived:
		return f.review(p)
	case change.FindingFiled:
		return f.finding(change.FindingRecord(v))
	case change.FindingVerified:
		return f.finding(change.FindingRecord(v))
	case change.FindingStateChanged:
		return f.finding(change.FindingRecord(v))
	case change.QuestionAsked, change.QuestionAnswered:
		return f.question(e, p)
	case change.GateEvaluated:
		return f.gate(e, v)
	}
	return strings.ReplaceAll(string(e.Type), ".", " ")
}

// triggered records the ticket and the repository of the change.
func (f *folder) triggered(v change.Triggered) string {
	f.d.Ticket = string(v.Ticket)
	f.d.Repo = string(v.Repo)
	return "triggered from " + string(v.Source)
}

// review sets the status of the reviewers named by a review payload and returns its note. It is called only with
// the four review payloads.
func (f *folder) review(p change.Payload) string {
	switch v := p.(type) {
	case change.ReviewersPicked:
		names := make([]string, 0, len(v.People))
		for _, person := range v.People {
			names = append(names, string(person))
			f.setReviewer(string(person), ReviewerWorking)
		}
		return "reviewers picked: " + strings.Join(names, ", ")
	case change.ReviewSubmitted:
		f.setReviewer(string(v.Reviewer), ReviewerSubmitted)
		return string(v.Reviewer) + " submitted a review"
	case change.ReviewUnavailable:
		f.setReviewer(string(v.Reviewer), ReviewerUnavailable)
		return string(v.Reviewer) + " unavailable: " + v.Reason
	case change.ReviewWaived:
		f.setReviewer(string(v.Reviewer), ReviewerWaived)
		return string(v.Reviewer) + " waived by " + string(v.Approver)
	}
	return ""
}

// question keeps the open questions and returns the note of a question payload. It is called only with the two
// question payloads.
func (f *folder) question(e change.Event, p change.Payload) string {
	switch v := p.(type) {
	case change.QuestionAsked:
		f.asks = append(f.asks, Ask{ID: string(v.QuestionID), Since: e.OccurredAt})
		return "question " + string(v.QuestionID) + " asked"
	case change.QuestionAnswered:
		f.asks = slices.DeleteFunc(f.asks, func(a Ask) bool { return a.ID == string(v.QuestionID) })
		return "question " + string(v.QuestionID) + " answered"
	}
	return ""
}

// gate keeps the latest gate decision and returns its note.
func (f *folder) gate(e change.Event, v change.GateEvaluated) string {
	f.d.Gate = Gate{Status: GatePassed, SHA: string(v.SHA), EvaluatedAt: e.OccurredAt}
	if !v.OK {
		f.d.Gate.Status = GateBlocked
		f.d.Gate.Reasons = slices.Clone(v.Reasons)
		return "gate blocked: " + strings.Join(v.Reasons, "; ")
	}
	return "gate passed"
}

// finding keeps the latest record of a finding and returns its note.
func (f *folder) finding(r change.FindingRecord) string {
	f.records[r.FindingID] = r
	return fmt.Sprintf("%s is %s (%s)", r.Ref, r.State, r.Tier)
}

// setReviewer sets the status of one reviewer.
func (f *folder) setReviewer(name, status string) {
	f.reviewers[name] = status
}

// result completes the read model after the fold.
func (f *folder) result() Detail {
	d := f.d
	if d.Gate.Status == GateBlocked {
		d.Blockers = slices.Clone(d.Gate.Reasons)
	}

	recs := slices.Collect(maps.Values(f.records))
	slices.SortFunc(recs, func(a, b change.FindingRecord) int { return cmp.Compare(a.Ref, b.Ref) })
	d.Findings = make([]Finding, 0, len(recs))
	for _, r := range recs {
		d.Findings = append(d.Findings, Finding{Ref: r.Ref.String(), Tier: string(r.Tier), State: string(r.State), By: string(r.By)})
	}

	for _, name := range slices.Sorted(maps.Keys(f.reviewers)) {
		d.Reviewers = append(d.Reviewers, Reviewer{Name: name, Status: f.reviewers[name]})
	}
	d.OpenAsks = slices.Clone(f.asks)
	return d
}
