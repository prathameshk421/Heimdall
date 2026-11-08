package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// WorkflowHistory enumerates durable change streams and reads their append-only histories.
type WorkflowHistory interface {
	ChangeIDs(context.Context) ([]id.ChangeID, error)
	Load(context.Context, id.ChangeID) ([]change.Event, error)
}

// EventTeamScope resolves the only groups allowed to receive one event's memory projection.
type EventTeamScope interface {
	TeamsForEvent(context.Context, change.Event, change.Repo, id.SHA, []string) ([]string, error)
}

type roomReader interface {
	Read(context.Context, change.RoomID, ports.Cursor) ([]ports.Message, ports.Cursor, error)
}

// MemoryRedactor removes secrets before workflow payloads can enter the durable memory queue.
type MemoryRedactor interface {
	Redact(string) (string, error)
}

// PolicyEventTeams maps an event's authenticated actor to policy teams at the change's captured base SHA.
type PolicyEventTeams struct {
	Code   PolicyCode
	Decode PolicyDecoder
}

// TeamsForEvent resolves only the policy-mapped human/assignee teams. Missing repository/base/identity fails closed.
func (p PolicyEventTeams) TeamsForEvent(ctx context.Context, event change.Event, repo change.Repo, base id.SHA, relatedActors []string) ([]string, error) {
	if p.Code == nil || p.Decode == nil || repo == "" || base == "" {
		return nil, nil
	}
	raw, err := p.Code.ReadFile(ctx, repo, base, "heimdall.yaml")
	if err != nil {
		return nil, fmt.Errorf("read workflow memory policy at base: %w", err)
	}
	policyAtBase, err := p.Decode(raw)
	if err != nil {
		return nil, fmt.Errorf("decode workflow memory policy at base: %w", err)
	}
	actors := []string{string(event.Actor.ID)}
	if event.Actor.Kind == change.ActorSystem {
		actors = append(actors, relatedActors...)
	}
	var teams []string
	for name, person := range policyAtBase.People {
		if personMatches(name, person, actors) {
			teams = append(teams, person.Teams...)
		}
	}
	slices.Sort(teams)
	return slices.Compact(teams), nil
}

func eventCandidateActors(events []change.Event) []string {
	for _, event := range events {
		if event.Type == change.EventChangeTriggered {
			payload, err := change.Decode(event)
			if err == nil {
				if value, ok := payload.(change.Triggered); ok {
					return []string{string(value.Assignee)}
				}
			}
		}
	}
	return nil
}

func personMatches(name string, person policy.Person, actors []string) bool {
	for _, actor := range actors {
		if actor != "" && (name == actor || person.GitHub == actor || person.Agent == actor || person.Chat == actor) {
			return true
		}
	}
	return false
}

// WorkflowMemoryStats describes the durable work accepted by a backfill pass.
type WorkflowMemoryStats struct{ Changes, Events, Queued int }

// WorkflowMemory projects high-signal durable workflow events into retryable Graphiti episodes.
type WorkflowMemory struct {
	queue   EpisodeEnqueuer
	history WorkflowHistory
	scope   EventTeamScope
	redact  MemoryRedactor
	clock   ports.Clock
	rooms   roomReader
}

// NewWorkflowMemory requires durable queue/history, a server policy scope, redaction and an injected clock.
func NewWorkflowMemory(queue EpisodeEnqueuer, history WorkflowHistory, scope EventTeamScope, redact MemoryRedactor, clock ports.Clock, rooms roomReader) (*WorkflowMemory, error) {
	if queue == nil || history == nil || scope == nil || redact == nil || clock == nil || rooms == nil {
		return nil, errors.New("workflow memory dependencies must not be nil")
	}
	return &WorkflowMemory{queue: queue, history: history, scope: scope, redact: redact, clock: clock, rooms: rooms}, nil
}

// Backfill replays all persisted histories. Stable event/team keys make repeated and concurrent passes idempotent.
// Each change is projected on its own: a change whose policy cannot be read is reported and skipped, so one old change
// can never keep every later change out of memory. The returned error joins every change that failed.
func (s *WorkflowMemory) Backfill(ctx context.Context) (WorkflowMemoryStats, error) {
	ids, err := s.history.ChangeIDs(ctx)
	if err != nil {
		return WorkflowMemoryStats{}, fmt.Errorf("list workflow histories for memory backfill: %w", err)
	}
	stats := WorkflowMemoryStats{Changes: len(ids)}
	var failed []error
	for _, changeID := range ids {
		if err := ctx.Err(); err != nil {
			return stats, err
		}
		if err := s.backfillChange(ctx, changeID, &stats); err != nil {
			failed = append(failed, fmt.Errorf("change %s: %w", changeID, err))
		}
	}
	return stats, errors.Join(failed...)
}

func (s *WorkflowMemory) backfillChange(ctx context.Context, changeID id.ChangeID, stats *WorkflowMemoryStats) error {
	events, err := s.history.Load(ctx, changeID)
	if err != nil {
		return fmt.Errorf("load workflow history for memory backfill: %w", err)
	}
	repo, base := eventRepositoryAndBase(events)
	relatedActors := eventCandidateActors(events)
	facts := FactsFromEvents(changeID, events)
	for _, event := range events {
		queued, err := s.ingest(ctx, event, repo, base, relatedActors, facts)
		if err != nil {
			return fmt.Errorf("project workflow event %s to memory: %w", event.ID, err)
		}
		stats.Events++
		if queued {
			stats.Queued++
		}
	}
	terminal := terminalEvent(events)
	if terminal == nil {
		return nil
	}
	roomID := eventRoomID(events)
	if roomID == "" {
		return nil
	}
	queued, err := s.digest(ctx, *terminal, roomID, repo, base, relatedActors)
	if err != nil {
		return fmt.Errorf("digest completed change: %w", err)
	}
	if queued {
		stats.Queued++
	}
	return nil
}

// Ingest projects a single event when it carries a durable human decision or an applied resolver verdict.
func (s *WorkflowMemory) Ingest(ctx context.Context, event change.Event, repo change.Repo, base id.SHA, relatedActors []string) (bool, error) {
	return s.ingest(ctx, event, repo, base, relatedActors, nil)
}

// ingest is Ingest with the facts of the change's findings, which the summary of the episode names.
func (s *WorkflowMemory) ingest(ctx context.Context, event change.Event, repo change.Repo, base id.SHA, relatedActors []string, facts FindingFacts) (bool, error) {
	status, ok := workflowMemoryStatus(event)
	if !ok {
		return false, nil
	}
	teams, err := s.scope.TeamsForEvent(ctx, event, repo, base, relatedActors)
	if err != nil {
		return false, fmt.Errorf("resolve workflow memory teams: %w", err)
	}
	teams = slices.Compact(slices.Clone(teams))
	if len(teams) == 0 {
		return false, nil
	}
	payload, err := s.redact.Redact(string(event.Payload))
	if err != nil {
		return false, fmt.Errorf("redact workflow memory payload: %w", err)
	}
	summary, err := s.redact.Redact(Narrate(event, facts))
	if err != nil {
		return false, fmt.Errorf("redact workflow memory summary: %w", err)
	}
	body, err := json.Marshal(struct {
		Summary    string           `json:"summary"`
		ChangeID   id.ChangeID      `json:"change_id"`
		EventID    change.EventID   `json:"event_id"`
		EventType  change.EventType `json:"event_type"`
		OccurredAt time.Time        `json:"occurred_at"`
		Payload    json.RawMessage  `json:"payload"`
	}{summary, event.ChangeID, event.ID, event.Type, event.OccurredAt.UTC(), json.RawMessage(payload)})
	if err != nil {
		return false, fmt.Errorf("encode workflow memory episode: %w", err)
	}
	queued := false
	for _, team := range teams {
		if team == "" {
			return false, errors.New("workflow memory scope returned an empty team")
		}
		episode := ports.Episode{Key: change.EffectKey(fmt.Sprintf("memory:workflow:v2:%s:%s", event.ID, team)), Source: string(event.Type), Status: status, GroupID: team, Body: string(body)}
		if err := episode.Validate(); err != nil {
			return false, fmt.Errorf("validate workflow memory episode: %w", err)
		}
		if err := s.queue.Enqueue(ctx, episode); err != nil {
			return false, fmt.Errorf("enqueue workflow memory episode: %w", err)
		}
		queued = true
	}
	return queued, nil
}

func (s *WorkflowMemory) digest(ctx context.Context, terminal change.Event, roomID change.RoomID, repo change.Repo, base id.SHA, actors []string) (bool, error) {
	teams, err := s.scope.TeamsForEvent(ctx, terminal, repo, base, actors)
	if err != nil {
		return false, fmt.Errorf("resolve digest team scope: %w", err)
	}
	teams = slices.Compact(slices.Clone(teams))
	if len(teams) == 0 {
		return false, nil
	}
	messages, _, err := s.rooms.Read(ctx, roomID, "")
	if err != nil {
		return false, fmt.Errorf("read completed change room: %w", err)
	}
	for i := range messages {
		messages[i].Text, err = s.redact.Redact(messages[i].Text)
		if err != nil {
			return false, fmt.Errorf("redact room digest message: %w", err)
		}
	}
	outcome, err := s.redact.Redact(string(terminal.Payload))
	if err != nil {
		return false, fmt.Errorf("redact room digest outcome: %w", err)
	}
	body, err := json.Marshal(struct {
		ChangeID       id.ChangeID      `json:"change_id"`
		RoomID         change.RoomID    `json:"room_id"`
		Outcome        change.EventType `json:"outcome"`
		OutcomeDetails json.RawMessage  `json:"outcome_details"`
		Messages       []ports.Message  `json:"messages"`
	}{terminal.ChangeID, roomID, terminal.Type, json.RawMessage(outcome), messages})
	if err != nil {
		return false, fmt.Errorf("encode room digest: %w", err)
	}
	for _, team := range teams {
		if team == "" {
			return false, errors.New("workflow memory scope returned an empty digest team")
		}
		episode := ports.Episode{Key: change.EffectKey(fmt.Sprintf("memory:digest:%s:%s", terminal.ID, team)), Source: "change-digest", Status: ports.EpisodeObserved, GroupID: team, Body: string(body)}
		if err := episode.Validate(); err != nil {
			return false, fmt.Errorf("validate room digest: %w", err)
		}
		if err := s.queue.Enqueue(ctx, episode); err != nil {
			return false, fmt.Errorf("enqueue room digest: %w", err)
		}
	}
	return true, nil
}

func eventRoomID(events []change.Event) change.RoomID {
	for _, event := range events {
		if event.Type == change.EventRoomOpened {
			payload, err := change.Decode(event)
			if err == nil {
				if opened, ok := payload.(change.RoomOpened); ok {
					return opened.RoomID
				}
			}
		}
	}
	return ""
}

func terminalEvent(events []change.Event) *change.Event {
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Type == change.EventChangeMerged || events[i].Type == change.EventChangeRejected {
			return &events[i]
		}
	}
	return nil
}

func eventRepositoryAndBase(events []change.Event) (change.Repo, id.SHA) {
	var repo change.Repo
	var base id.SHA
	for _, event := range events {
		switch event.Type {
		case change.EventChangeTriggered:
			if payload, err := change.Decode(event); err == nil {
				if triggered, ok := payload.(change.Triggered); ok {
					repo = triggered.Repo
				}
			}
		case change.EventBranchEnsured:
			if payload, err := change.Decode(event); err == nil {
				if branch, ok := payload.(change.BranchEnsured); ok {
					base = branch.BaseSHA
				}
			}
		// Remaining event kinds carry no workflow memory signal.
		case change.EventRoomOpened, change.EventTaskAssigned, change.EventTaskAcked,
			change.EventTaskTimedOut, change.EventTaskFailed, change.EventTaskProgress,
			change.EventStepRetried, change.EventStepEscalated, change.EventStepResumed, change.EventQuestionAsked,
			change.EventQuestionAnswered, change.EventTestsSubmitted, change.EventTestsVerified,
			change.EventTestsRejected, change.EventTestsCommitted, change.EventTestsApproved,
			change.EventTicketChanged, change.EventPROpened, change.EventPRPushed, change.EventPRLinked,
			change.EventPRClosed, change.EventSlopEvaluated, change.EventReviewersPicked,
			change.EventReviewSubmitted, change.EventReviewUnavailable, change.EventReviewWaived,
			change.EventFindingFiled, change.EventFindingVerified, change.EventFindingStateChanged,
			change.EventChallengeFiled, change.EventCaseOpened, change.EventVerdictSubmitted,
			change.EventVerdictApplied, change.EventVerdictEscalated, change.EventDecisionRecorded,
			change.EventApprovalRecorded, change.EventApprovalInvalidated, change.EventCIReported,
			change.EventGateEvaluated, change.EventChangeRejected, change.EventChangeMerged:
		}
	}
	return repo, base
}

func workflowMemoryStatus(event change.Event) (ports.EpisodeStatus, bool) {
	switch event.Type {
	case change.EventDecisionRecorded:
		return decisionMemoryStatus(event)
	case change.EventVerdictApplied:
		if _, err := change.Decode(event); err == nil {
			return ports.EpisodeConfirmed, true
		}
	case change.EventFindingStateChanged:
		return findingStateMemoryStatus(event)
	// Remaining event kinds carry no workflow memory signal.
	case change.EventChangeTriggered, change.EventRoomOpened, change.EventBranchEnsured,
		change.EventTaskAssigned, change.EventTaskAcked, change.EventTaskTimedOut,
		change.EventTaskFailed, change.EventTaskProgress, change.EventStepRetried,
		change.EventStepEscalated, change.EventStepResumed, change.EventQuestionAsked, change.EventQuestionAnswered,
		change.EventTestsSubmitted, change.EventTestsVerified, change.EventTestsRejected,
		change.EventTestsCommitted, change.EventTestsApproved, change.EventTicketChanged,
		change.EventPROpened, change.EventPRPushed, change.EventPRLinked, change.EventPRClosed,
		change.EventSlopEvaluated, change.EventReviewersPicked, change.EventReviewSubmitted,
		change.EventReviewUnavailable, change.EventReviewWaived, change.EventFindingFiled,
		change.EventFindingVerified, change.EventChallengeFiled,
		change.EventCaseOpened, change.EventVerdictSubmitted, change.EventVerdictEscalated,
		change.EventApprovalRecorded, change.EventApprovalInvalidated, change.EventCIReported,
		change.EventGateEvaluated, change.EventChangeRejected, change.EventChangeMerged:
	}
	return "", false
}

// decisionMemoryStatus covers a decision a person made. Notes and challenges are observed, other decisions are confirmed.
func decisionMemoryStatus(event change.Event) (ports.EpisodeStatus, bool) {
	if event.Actor.Kind != change.ActorHuman {
		return "", false
	}
	payload, err := change.Decode(event)
	if err != nil {
		return "", false
	}
	decision, ok := payload.(change.DecisionRecorded)
	if !ok || decision.Kind == change.DecisionForget {
		return "", false
	}
	if slices.Contains([]change.DecisionKind{change.DecisionRemember, change.DecisionIncident, change.DecisionChallenge}, decision.Kind) {
		return ports.EpisodeObserved, true
	}
	return ports.EpisodeConfirmed, true
}

// findingStateMemoryStatus: a finding that people upheld or that was resolved is durable knowledge; other states are noise.
func findingStateMemoryStatus(event change.Event) (ports.EpisodeStatus, bool) {
	payload, err := change.Decode(event)
	if err != nil {
		return "", false
	}
	value, ok := payload.(change.FindingStateChanged)
	if ok && (value.State == finding.StateUpheld || value.State == finding.StateResolved) {
		return ports.EpisodeObserved, true
	}
	return "", false
}
