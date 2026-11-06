package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// WorkflowScopes returns only server-derived memory groups for one trusted workflow event.
type WorkflowScopes interface {
	GroupsForEvent(context.Context, change.Event) ([]string, error)
}

// WorkflowIngestor records selected durable workflow events in the source/trust queue.
type WorkflowIngestor struct {
	queue      ports.EpisodeQueue
	scopes     WorkflowScopes
	scanner    FileScanner
	processors []WorkflowProcessor
}

// WorkflowCursor stores the last globally sequenced event whose memory projection was queued.
type WorkflowCursor interface {
	LoadWorkflowCursor(context.Context) (int64, error)
	SaveWorkflowCursor(context.Context, int64) error
}

// Backfill replays durable event history after the saved cursor. It advances the cursor only after every event in a
// page is queued, so a restart can safely replay the page using stable event/group keys.
func (i *WorkflowIngestor) Backfill(ctx context.Context, history ports.WorkflowHistory, cursor WorkflowCursor, batch int) (int, error) {
	if err := validateBackfill(history, cursor, batch); err != nil {
		return 0, err
	}
	after, err := cursor.LoadWorkflowCursor(ctx)
	if err != nil {
		return 0, fmt.Errorf("load workflow memory cursor: %w", err)
	}
	queued := 0
	for {
		if err := ctx.Err(); err != nil {
			return queued, err
		}
		events, next, err := readWorkflowPage(ctx, history, after, batch)
		if err != nil {
			return queued, err
		}
		if len(events) == 0 {
			return queued, nil
		}
		count, err := i.Record(ctx, events)
		if err != nil {
			return queued, err
		}
		if err := cursor.SaveWorkflowCursor(ctx, next); err != nil {
			return queued, fmt.Errorf("save workflow memory cursor: %w", err)
		}
		after = next
		queued += count
		if len(events) < batch {
			return queued, nil
		}
	}
}

func validateBackfill(history ports.WorkflowHistory, cursor WorkflowCursor, batch int) error {
	if history == nil || cursor == nil || batch < 1 || batch > 1000 {
		return errors.New("workflow backfill dependencies or batch size are invalid")
	}
	return nil
}

// readWorkflowPage reads one page after the cursor. An empty page is returned as-is; a non-empty page must advance.
func readWorkflowPage(ctx context.Context, history ports.WorkflowHistory, after int64, batch int) ([]change.Event, int64, error) {
	events, next, err := history.EventsAfter(ctx, after, batch)
	if err != nil {
		return nil, 0, fmt.Errorf("read workflow memory history: %w", err)
	}
	if len(events) > 0 && next <= after {
		return nil, 0, errors.New("workflow history cursor did not advance")
	}
	return events, next, nil
}

// NewWorkflowIngestor requires durable persistence, server scope resolution, and redaction.
func NewWorkflowIngestor(queue ports.EpisodeQueue, scopes WorkflowScopes, scanner FileScanner, processors ...WorkflowProcessor) (*WorkflowIngestor, error) {
	if queue == nil || scopes == nil || scanner == nil {
		return nil, errors.New("workflow memory ingestion dependencies must not be nil")
	}
	for _, processor := range processors {
		if processor == nil {
			return nil, errors.New("workflow memory processor must not be nil")
		}
	}
	return &WorkflowIngestor{queue: queue, scopes: scopes, scanner: scanner, processors: append([]WorkflowProcessor(nil), processors...)}, nil
}

// Record persists eligible episodes before any worker sends them to Graphiti. Replaying an event is safe because
// each event/group pair has a stable key and the catalog rejects changed source content under that key.
func (i *WorkflowIngestor) Record(ctx context.Context, events []change.Event) (int, error) {
	queued := 0
	for _, event := range events {
		if err := ctx.Err(); err != nil {
			return queued, err
		}
		count, err := i.recordEvent(ctx, event)
		queued += count
		if err != nil {
			return queued, err
		}
	}
	return queued, nil
}

// recordEvent queues the event's memory episodes and then runs the other processors. The returned count is the
// number of episodes queued before any error.
func (i *WorkflowIngestor) recordEvent(ctx context.Context, event change.Event) (int, error) {
	status, eligible, err := workflowTrust(event)
	if err != nil {
		return 0, fmt.Errorf("classify workflow memory event %s: %w", event.ID, err)
	}
	if !eligible {
		return 0, i.process(ctx, event)
	}
	groups, err := i.eventGroups(ctx, event, status)
	if err != nil {
		return 0, err
	}
	queued, err := i.queueEpisodes(ctx, event, status, groups)
	if err != nil {
		return queued, err
	}
	return queued, i.process(ctx, event)
}

// eventGroups resolves the server-derived memory groups for an eligible event; confirmed events also join "shared".
func (i *WorkflowIngestor) eventGroups(ctx context.Context, event change.Event, status ports.EpisodeStatus) ([]string, error) {
	groups, err := i.scopes.GroupsForEvent(ctx, event)
	if err != nil {
		return nil, fmt.Errorf("resolve memory groups for event %s: %w", event.ID, err)
	}
	if status == ports.EpisodeConfirmed {
		groups = append(groups, "shared")
	}
	groups = uniqueSorted(groups)
	if len(groups) == 0 {
		return nil, fmt.Errorf("workflow event %s has no server-derived memory group", event.ID)
	}
	return groups, nil
}

func (i *WorkflowIngestor) queueEpisodes(ctx context.Context, event change.Event, status ports.EpisodeStatus, groups []string) (int, error) {
	queued := 0
	for _, group := range groups {
		episode, err := buildWorkflowEpisode(event, status, group, i.scanner)
		if err != nil {
			return queued, fmt.Errorf("build memory episode for event %s: %w", event.ID, err)
		}
		if err := i.queue.EnqueueEpisode(ctx, episode); err != nil {
			return queued, fmt.Errorf("queue memory episode for event %s: %w", event.ID, err)
		}
		queued++
	}
	return queued, nil
}

func (i *WorkflowIngestor) process(ctx context.Context, event change.Event) error {
	for _, processor := range i.processors {
		if err := processor.ProcessWorkflowEvent(ctx, event); err != nil {
			return fmt.Errorf("project workflow event %s: %w", event.ID, err)
		}
	}
	return nil
}

func workflowTrust(event change.Event) (ports.EpisodeStatus, bool, error) {
	if err := event.Validate(); err != nil {
		return "", false, err
	}
	switch event.Type {
	case change.EventDecisionRecorded, change.EventVerdictApplied:
		return ports.EpisodeConfirmed, true, nil
	case change.EventChangeTriggered, change.EventTicketChanged, change.EventChangeMerged, change.EventChangeRejected:
		return ports.EpisodeObserved, true, nil
	case change.EventFindingStateChanged:
		payload, err := change.Decode(event)
		if err != nil {
			return "", false, err
		}
		state := payload.(change.FindingStateChanged).State
		return ports.EpisodeObserved, state == "resolved" || state == "upheld", nil
	case change.EventRoomOpened, change.EventBranchEnsured, change.EventTaskAssigned, change.EventTaskAcked,
		change.EventTaskTimedOut, change.EventTaskFailed, change.EventStepRetried, change.EventStepEscalated,
		change.EventQuestionAsked, change.EventQuestionAnswered, change.EventTestsSubmitted, change.EventTestsVerified,
		change.EventTestsRejected, change.EventTestsCommitted, change.EventTestsApproved, change.EventPROpened,
		change.EventPRPushed, change.EventPRLinked, change.EventPRClosed, change.EventSlopEvaluated,
		change.EventReviewersPicked, change.EventReviewSubmitted, change.EventReviewUnavailable, change.EventReviewWaived,
		change.EventFindingFiled, change.EventFindingVerified, change.EventChallengeFiled, change.EventCaseOpened,
		change.EventVerdictSubmitted, change.EventVerdictEscalated, change.EventApprovalRecorded,
		change.EventApprovalInvalidated, change.EventCIReported, change.EventGateEvaluated:
		return ports.EpisodeObserved, false, nil
	}
	// Validate rejects types outside the catalog, so every validated event has a case above.
	return ports.EpisodeObserved, false, nil
}

func buildWorkflowEpisode(event change.Event, status ports.EpisodeStatus, group string, scanner FileScanner) (ports.Episode, error) {
	payload, err := json.Marshal(struct {
		SourceRef  string          `json:"source_ref"`
		EventType  string          `json:"event_type"`
		ChangeID   string          `json:"change_id"`
		OccurredAt string          `json:"occurred_at"`
		Actor      change.Actor    `json:"actor"`
		Payload    json.RawMessage `json:"payload"`
	}{
		SourceRef:  "heimdall:event:" + string(event.ID),
		EventType:  string(event.Type),
		ChangeID:   string(event.ChangeID),
		OccurredAt: event.OccurredAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"),
		Actor:      event.Actor,
		Payload:    event.Payload,
	})
	if err != nil {
		return ports.Episode{}, fmt.Errorf("encode workflow memory source: %w", err)
	}
	clean, _, err := scanner.Scan(string(payload))
	if err != nil {
		return ports.Episode{}, fmt.Errorf("redact workflow memory source: %w", err)
	}
	key := change.EffectKey("workflow:" + string(event.ID) + ":" + group)
	episode := ports.Episode{Key: key, Source: string(event.Type), Status: status, GroupID: group, Body: clean}
	if err := episode.Validate(); err != nil {
		return ports.Episode{}, fmt.Errorf("validate workflow memory episode: %w", err)
	}
	return episode, nil
}
