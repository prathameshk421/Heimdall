package agenttools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// Constants governing agent tool operations.
const (
	AckWindow  = 10 * time.Minute
	MaxRetries = 5
)

// Standard typed errors.
var (
	ErrNoTask           = errors.New("no active task")
	ErrWrongAgent       = errors.New("wrong agent for task")
	ErrWrongRole        = errors.New("wrong role for task")
	ErrAckWindowExpired = errors.New("acknowledgement window expired")
	ErrLateSubmission   = errors.New("submission is late")
	ErrReducerRefusal   = errors.New("reducer refused event")
	ErrVersionConflict  = errors.New("version conflict after max retries")
	ErrValidation       = errors.New("validation failed")
)

// Assignment is the active assignment of an agent, reconstructed from domain events.
type Assignment struct {
	ChangeID   id.ChangeID
	TaskID     id.TaskID
	Agent      id.AgentName
	Role       workflow.Role
	Deadline   time.Time
	BundleRef  workflow.BlobRef
	AssignedAt time.Time
	Acked      bool
	// History is this ticket's story so far, in plain lines; the agent reads it with get_task.
	History []string
	Task    workflow.TaskEnvelope
}

// FindingError reports a validation failure for one finding in a review.
type FindingError struct {
	Index   int    `json:"index"`
	Message string `json:"message"`
}

// Receipt is the outcome of a review submission.
type Receipt struct {
	Accepted      bool           `json:"accepted"`
	FindingErrors []FindingError `json:"finding_errors,omitempty"`
}

// ReviewRefs provides the caller-scoped references used by semantic review validation.
type ReviewRefs interface {
	FileExists(context.Context, workflow.ChangeRef, string) (bool, error)
	LineCount(context.Context, workflow.ChangeRef, string) (int, error)
	RunExists(context.Context, workflow.ChangeRef, id.AgentName, string) (bool, error)
	MemoryExists(context.Context, workflow.ChangeRef, id.AgentName, string) (bool, error)
	PackExists(context.Context, workflow.ChangeRef, id.AgentName, string) (bool, error)
}

// ReviewSubmission is the trusted scope supplied to semantic review validation.
type ReviewSubmission struct {
	Change       workflow.ChangeRef
	Caller       id.AgentName
	FilesChanged []string
	Review       finding.Review
}

// ReviewFinding is a semantically accepted finding with its calculated evidence tier.
type ReviewFinding struct {
	Input finding.Input
	Tier  finding.Tier
}

// ReviewProblem is one correctable semantic review error.
type ReviewProblem struct{ Message string }

// ReviewResult is returned by the semantic review validator.
type ReviewResult struct {
	Findings []ReviewFinding
	Problems []ReviewProblem
}

// ReviewValidator validates and merges one review submission at the application boundary.
type ReviewValidator interface {
	Submit(context.Context, ReviewSubmission) (ReviewResult, error)
}

// Asker posts human-facing questions to the room and returns identifiers.
type Asker interface {
	Ask(ctx context.Context, changeID id.ChangeID, question string) (change.QuestionID, change.SenderID, change.MessageID, error)
}

// Config configures the agent tools Service. Every field is required.
type Config struct {
	Events          ports.EventStore
	Index           ports.TaskIndex
	Blobs           ports.BlobStore
	Clock           ports.Clock
	IDs             ports.IDs
	Asker           Asker
	ReviewRefs      ReviewRefs
	ReviewValidator ReviewValidator
	// Advance is optional. When set, a review submission hands its events to it instead of appending them to the
	// store, so the workflow engine stores them and offers them to its steps. The verify step needs this to see the
	// findings it must verify (A2).
	Advance func(context.Context, id.ChangeID, []change.Event) error
	// PRFiles is optional. When set, a reviewer's and a resolver's task names the diff and the changed files of the
	// pull request, so they know what to read and a finding on a changed file counts as inside the diff.
	PRFiles PRFiles
}

// Service implements application logic for agent tools.
type Service struct {
	events ports.EventStore
	index  ports.TaskIndex
	blobs  ports.BlobStore
	clock  ports.Clock
	ids    ports.IDs
	asker  Asker
	review ReviewValidator

	advance func(context.Context, id.ChangeID, []change.Event) error

	prFiles PRFiles
	prCache prInputCache
}

// New validates dependencies and constructs a Service.
func New(cfg Config) (*Service, error) {
	var missing []string
	if cfg.Events == nil {
		missing = append(missing, "Events")
	}
	if cfg.Index == nil {
		missing = append(missing, "Index")
	}
	if cfg.Blobs == nil {
		missing = append(missing, "Blobs")
	}
	if cfg.Clock == nil {
		missing = append(missing, "Clock")
	}
	if cfg.IDs == nil {
		missing = append(missing, "IDs")
	}
	if cfg.Asker == nil {
		missing = append(missing, "Asker")
	}
	if cfg.ReviewRefs == nil {
		missing = append(missing, "ReviewRefs")
	}
	if cfg.ReviewValidator == nil {
		missing = append(missing, "ReviewValidator")
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("agenttools: nil dependency: %s", strings.Join(missing, ", "))
	}
	return &Service{
		events: cfg.Events,
		index:  cfg.Index,
		blobs:  cfg.Blobs,
		clock:  cfg.Clock,
		ids:    cfg.IDs,
		asker:  cfg.Asker,
		review: cfg.ReviewValidator,

		advance: cfg.Advance,
		prFiles: cfg.PRFiles,
	}, nil
}

// appendEvents executes the retry loop on ports.ErrVersionConflict (up to MaxRetries attempts).
func (s *Service) appendEvents(ctx context.Context, cid id.ChangeID, fn func(existing []change.Event, state change.State) ([]change.Event, error)) error {
	return s.appendEventsWith(ctx, cid, fn, nil)
}

// appendEventsWith is appendEvents with an optional hook that stores the events in place of the event store. A hook
// error is returned as it is; a version conflict from it takes the retry loop again.
func (s *Service) appendEventsWith(ctx context.Context, cid id.ChangeID, fn func(existing []change.Event, state change.State) ([]change.Event, error), hook func(context.Context, id.ChangeID, []change.Event) error) error {
	for attempt := 0; attempt < MaxRetries; attempt++ {
		existing, err := s.events.Load(ctx, cid)
		if err != nil {
			return fmt.Errorf("load %s: %w", cid, err)
		}
		state, err := change.Replay(existing)
		if err != nil {
			return fmt.Errorf("stream %s does not replay: %w", cid, err)
		}

		evs, err := fn(existing, state)
		if err != nil {
			return err
		}
		if len(evs) == 0 {
			return nil
		}

		currState := state
		for _, ev := range evs {
			nextState, err := change.Reduce(currState, ev)
			if err != nil {
				return fmt.Errorf("%w: %s refused by reducer in state %s: %w", ErrReducerRefusal, ev.Type, currState, err)
			}
			currState = nextState
		}

		expected := int64(len(existing))
		if hook != nil {
			err = hook(ctx, cid, evs)
		} else {
			_, err = s.events.Append(ctx, cid, expected, evs, nil)
		}
		if err == nil {
			return nil
		}
		if errors.Is(err, ports.ErrVersionConflict) {
			continue
		}
		return fmt.Errorf("append to %s: %w", cid, err)
	}
	return fmt.Errorf("%w: after %d attempts: %w", ErrVersionConflict, MaxRetries, ports.ErrVersionConflict)
}

func (s *Service) buildEventWithID(
	eid change.EventID,
	cid id.ChangeID,
	version int64,
	et change.EventType,
	agent id.AgentName,
	corr change.CorrelationID,
	causation *change.EventID,
	payload change.Payload,
	blobRefs []workflow.BlobRef,
) (change.Event, error) {
	if err := payload.Validate(); err != nil {
		return change.Event{}, fmt.Errorf("payload of %s: %w", et, err)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return change.Event{}, fmt.Errorf("encode payload of %s: %w", et, err)
	}
	ev := change.Event{
		ID:            eid,
		ChangeID:      cid,
		Version:       version,
		Type:          et,
		Actor:         change.Actor{Kind: change.ActorAgent, ID: change.ActorID(agent)},
		OccurredAt:    s.clock.Now().UTC(),
		CorrelationID: corr,
		CausationID:   causation,
		BlobRefs:      blobRefs,
		Payload:       raw,
	}
	if err := ev.Validate(); err != nil {
		return change.Event{}, fmt.Errorf("event %s: %w", et, err)
	}
	return ev, nil
}

func lastCausation(existing []change.Event) *change.EventID {
	if len(existing) == 0 {
		return nil
	}
	return &existing[len(existing)-1].ID
}

func correlationIDOf(existing []change.Event, fallback change.EventID) change.CorrelationID {
	if len(existing) > 0 && existing[len(existing)-1].CorrelationID != "" {
		return existing[len(existing)-1].CorrelationID
	}
	return change.CorrelationID(fallback.String())
}

func findTaskAssigned(events []change.Event, taskID id.TaskID) (change.TaskAssigned, bool) {
	for i := len(events) - 1; i >= 0; i-- {
		ev := events[i]
		if ev.Type == change.EventTaskAssigned {
			if p, err := change.Decode(ev); err == nil {
				if ta, ok := p.(change.TaskAssigned); ok && ta.TaskID == taskID {
					return ta, true
				}
			}
		}
	}
	return change.TaskAssigned{}, false
}

func hasTaskAcked(events []change.Event, taskID id.TaskID) bool {
	for _, ev := range events {
		if ev.Type == change.EventTaskAcked {
			if p, err := change.Decode(ev); err == nil {
				if ta, ok := p.(change.TaskAcked); ok && ta.TaskID == taskID {
					return true
				}
			}
		}
	}
	return false
}
