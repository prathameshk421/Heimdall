package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// WorkflowProcessor adds durable projections that require other trusted workflow sources.
type WorkflowProcessor interface {
	ProcessWorkflowEvent(context.Context, change.Event) error
}

// WorkflowModelResolver chooses the extraction model that policy allows for one trusted workflow event.
type WorkflowModelResolver interface {
	ExtractionModelForEvent(context.Context, change.Event) (string, error)
}

// RoomDigest produces a source-linked observed room summary and separately queues confirmed decisions from the event log.
type RoomDigest struct {
	store     ports.EventStore
	chat      ports.Chat
	summarize ports.RoomSummarizer
	queue     ports.EpisodeQueue
	scopes    WorkflowScopes
	models    WorkflowModelResolver
	scanner   FileScanner
}

// NewRoomDigest requires every collaborator so a digest cannot skip redaction, policy scope or durable queueing.
func NewRoomDigest(store ports.EventStore, chat ports.Chat, summarize ports.RoomSummarizer, queue ports.EpisodeQueue, scopes WorkflowScopes, models WorkflowModelResolver, scanner FileScanner) (*RoomDigest, error) {
	if store == nil || chat == nil || summarize == nil || queue == nil || scopes == nil || models == nil || scanner == nil {
		return nil, errors.New("room digest dependencies must not be nil")
	}
	return &RoomDigest{store: store, chat: chat, summarize: summarize, queue: queue, scopes: scopes, models: models, scanner: scanner}, nil
}

// ProcessWorkflowEvent queues the room digest and confirmed decisions for a merged or rejected change.
func (d *RoomDigest) ProcessWorkflowEvent(ctx context.Context, event change.Event) error {
	if event.Type != change.EventChangeMerged && event.Type != change.EventChangeRejected {
		return nil
	}
	room, decisions, err := closedChangeRoom(ctx, d.store, event)
	if err != nil {
		return err
	}
	groups, err := d.scopes.GroupsForEvent(ctx, event)
	if err != nil {
		return fmt.Errorf("resolve room digest group: %w", err)
	}
	if len(groups) == 0 {
		return fmt.Errorf("closed change %s has no policy team for its room digest", event.ChangeID)
	}
	messages, err := d.redactedRoomMessages(ctx, room)
	if err != nil {
		return err
	}
	observed, err := d.observedRoomSummary(ctx, event, messages)
	if err != nil {
		return err
	}
	for _, group := range uniqueSorted(groups) {
		if err := d.queueConfirmedDecisions(ctx, event, decisions, group); err != nil {
			return err
		}
		if err := d.queueObservedDigest(ctx, event, room, messages, observed, group); err != nil {
			return err
		}
	}
	return nil
}

// closedChangeRoom reads the change's stream and returns its room and the decisions recorded in it.
func closedChangeRoom(ctx context.Context, store ports.EventStore, event change.Event) (change.RoomID, []json.RawMessage, error) {
	stream, err := store.Load(ctx, event.ChangeID)
	if err != nil {
		return "", nil, fmt.Errorf("load closed change room history: %w", err)
	}
	var room change.RoomID
	var decisions []json.RawMessage
	for _, item := range stream {
		if item.Type == change.EventRoomOpened {
			payload, err := change.Decode(item)
			if err != nil {
				return "", nil, fmt.Errorf("decode room source: %w", err)
			}
			room = payload.(change.RoomOpened).RoomID
		}
		if item.Type == change.EventDecisionRecorded {
			decisions = append(decisions, append(json.RawMessage(nil), item.Payload...))
		}
	}
	if room == "" {
		return "", nil, fmt.Errorf("closed change %s has no room source", event.ChangeID)
	}
	return room, decisions, nil
}

// redactedRoomMessages reads the closed room and redacts every message before any model sees it.
func (d *RoomDigest) redactedRoomMessages(ctx context.Context, room change.RoomID) ([]ports.Message, error) {
	messages, _, err := d.chat.Read(ctx, room, "")
	if err != nil {
		return nil, fmt.Errorf("read closed change room: %w", err)
	}
	for n := range messages {
		if err := messages[n].Validate(); err != nil {
			return nil, fmt.Errorf("validate room message %s: %w", messages[n].ID, err)
		}
		clean, _, err := d.scanner.Scan(messages[n].Text)
		if err != nil {
			return nil, fmt.Errorf("redact room message %s: %w", messages[n].ID, err)
		}
		messages[n].Text = clean
	}
	return messages, nil
}

// observedRoomSummary summarizes the redacted room with the policy-selected model. A room without messages has no summary.
func (d *RoomDigest) observedRoomSummary(ctx context.Context, event change.Event, messages []ports.Message) (string, error) {
	if len(messages) == 0 {
		return "", nil
	}
	model, err := d.models.ExtractionModelForEvent(ctx, event)
	if err != nil {
		return "", fmt.Errorf("resolve room digest model from policy: %w", err)
	}
	observed, err := d.summarize.SummarizeRoom(ctx, model, messages)
	if err != nil {
		return "", fmt.Errorf("summarize redacted change room: %w", err)
	}
	observed, _, err = d.scanner.Scan(observed)
	if err != nil {
		return "", fmt.Errorf("redact room digest: %w", err)
	}
	return observed, nil
}

// queueConfirmedDecisions queues the change's recorded decisions for one memory group as confirmed knowledge.
// A change without recorded decisions queues nothing.
func (d *RoomDigest) queueConfirmedDecisions(ctx context.Context, event change.Event, decisions []json.RawMessage, group string) error {
	if len(decisions) == 0 {
		return nil
	}
	body, err := json.Marshal(struct {
		SourceRef string            `json:"source_ref"`
		Event     string            `json:"event"`
		Decisions []json.RawMessage `json:"confirmed_decisions"`
	}{
		SourceRef: "heimdall:room-decisions:" + string(event.ID), Event: string(event.Type), Decisions: decisions,
	})
	if err != nil {
		return fmt.Errorf("encode confirmed room decisions: %w", err)
	}
	clean, _, err := d.scanner.Scan(string(body))
	if err != nil {
		return fmt.Errorf("redact confirmed room decisions: %w", err)
	}
	episode := ports.Episode{Key: change.EffectKey("room-digest:" + string(event.ID) + ":confirmed:" + group), Source: "change-room", Status: ports.EpisodeConfirmed, GroupID: group, Body: clean}
	if err := d.queue.EnqueueEpisode(ctx, episode); err != nil {
		return fmt.Errorf("queue confirmed room decisions: %w", err)
	}
	return nil
}

// queueObservedDigest queues the observed room summary for one memory group, linked to the message ids it came from.
// A room without a summary queues nothing.
func (d *RoomDigest) queueObservedDigest(ctx context.Context, event change.Event, room change.RoomID, messages []ports.Message, observed string, group string) error {
	if observed == "" {
		return nil
	}
	refs := make([]string, 0, len(messages))
	for _, message := range messages {
		refs = append(refs, string(message.ID))
	}
	body, err := json.Marshal(struct {
		SourceRef string   `json:"source_ref"`
		Event     string   `json:"event"`
		Messages  []string `json:"messages"`
		Summary   string   `json:"observed_summary"`
	}{
		SourceRef: "heimdall:room:" + string(room), Event: string(event.Type), Messages: refs, Summary: observed,
	})
	if err != nil {
		return fmt.Errorf("encode observed room digest: %w", err)
	}
	episode := ports.Episode{Key: change.EffectKey("room-digest:" + string(event.ID) + ":observed:" + group), Source: "change-room", Status: ports.EpisodeObserved, GroupID: group, Body: string(body)}
	if err := d.queue.EnqueueEpisode(ctx, episode); err != nil {
		return fmt.Errorf("queue observed room digest: %w", err)
	}
	return nil
}

var _ WorkflowProcessor = (*RoomDigest)(nil)
