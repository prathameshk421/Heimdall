package agenttools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/testset"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// Current finds the active task through the index and rebuilds the assignment from the latest task.assigned.
func (s *Service) Current(ctx context.Context, agent id.AgentName, role workflow.Role) (Assignment, error) {
	ref, err := s.index.ActiveTask(ctx, agent, role)
	if err != nil {
		if errors.Is(err, ports.ErrNotFound) {
			return Assignment{}, fmt.Errorf("%w: %w", ErrNoTask, ports.ErrNotFound)
		}
		return Assignment{}, fmt.Errorf("lookup active task: %w", err)
	}

	events, err := s.events.Load(ctx, ref.ChangeID)
	if err != nil {
		return Assignment{}, fmt.Errorf("load change %s: %w", ref.ChangeID, err)
	}

	latestTask, assignedAt, acked := findActiveTaskInEvents(events, ref.TaskID)
	if latestTask == nil {
		return Assignment{}, fmt.Errorf("%w: task %s not found in change %s", ErrNoTask, ref.TaskID, ref.ChangeID)
	}
	if taskFinished(events, *latestTask) {
		return Assignment{}, ErrNoTask
	}
	if err := validateCurrentTask(latestTask, agent, role); err != nil {
		return Assignment{}, err
	}

	return buildAssignment(ref, latestTask, assignedAt, acked, events), nil
}

func findActiveTaskInEvents(events []change.Event, taskID id.TaskID) (*change.TaskAssigned, time.Time, bool) {
	var latestTask *change.TaskAssigned
	var assignedAt time.Time
	var acked bool

	for _, ev := range events {
		p, err := change.Decode(ev)
		if err != nil {
			continue
		}
		switch v := p.(type) {
		case change.TaskAssigned:
			if v.TaskID == taskID {
				copyV := v
				latestTask = &copyV
				assignedAt = ev.OccurredAt
			}
		case change.TaskAcked:
			if latestTask != nil && v.TaskID == taskID && v.Agent == latestTask.Agent && v.Role == latestTask.Role {
				acked = true
			}
		}
	}
	return latestTask, assignedAt, acked
}

func validateCurrentTask(latest *change.TaskAssigned, agent id.AgentName, role workflow.Role) error {
	if latest.Agent != agent {
		return ErrWrongAgent
	}
	if latest.Role != role {
		return ErrWrongRole
	}
	return nil
}

func buildAssignment(ref ports.ActiveTaskRef, latest *change.TaskAssigned, assignedAt time.Time, acked bool, events []change.Event) Assignment {
	criteria := make([]workflow.CriterionRef, 0)
	untrusted := make([]workflow.UntrustedRef, 0)
	changeRef := workflow.ChangeRef{ID: ref.ChangeID}
	for _, ev := range events {
		payload, err := change.Decode(ev)
		if err != nil {
			continue
		}
		switch p := payload.(type) {
		case change.Triggered:
			changeRef.Repo = string(p.Repo)
			for _, criterion := range p.Criteria {
				criteria = append(criteria, workflow.CriterionRef{ID: criterion.ID, Ref: criterion.Ref})
			}
			if p.DescriptionRef != "" {
				untrusted = append(untrusted, workflow.UntrustedRef{Source: "ticket description", Ref: p.DescriptionRef})
			}
		case change.BranchEnsured:
			changeRef.Repo = string(p.Repo)
			changeRef.BaseSHA = p.BaseSHA
		case change.PROpened:
			changeRef.HeadSHA = p.HeadSHA
		case change.PRPushed:
			changeRef.HeadSHA = p.HeadSHA
		}
	}
	return Assignment{
		ChangeID:   ref.ChangeID,
		TaskID:     latest.TaskID,
		Agent:      latest.Agent,
		Role:       latest.Role,
		Deadline:   latest.Deadline,
		BundleRef:  latest.BundleRef,
		AssignedAt: assignedAt,
		Acked:      acked,
		Task: workflow.TaskEnvelope{
			TaskID:   latest.TaskID,
			CaseID:   latest.CaseID,
			Role:     latest.Role,
			Deadline: latest.Deadline,
			Change:   changeRef,
			Inputs:   workflow.Inputs{AcceptanceCriteria: criteria, Untrusted: untrusted, Answers: collectAnswers(events)},
		},
	}
}

func checkTaskAgent(existing []change.Event, a Assignment) error {
	assigned, found := findTaskAssigned(existing, a.TaskID)
	if !found {
		return fmt.Errorf("%w: task %s", ErrNoTask, a.TaskID)
	}
	if assigned.Agent != a.Agent {
		return ErrWrongAgent
	}
	if assigned.Role != a.Role {
		return ErrWrongRole
	}
	if taskFinished(existing, assigned) {
		return ErrNoTask
	}
	return nil
}

func taskFinished(events []change.Event, assigned change.TaskAssigned) bool {
	for _, ev := range events {
		if ev.Type != change.EventTaskTimedOut && ev.Type != change.EventTaskFailed {
			continue
		}
		p, err := change.Decode(ev)
		if err != nil {
			continue
		}
		if taskOutcomeMatches(p, assigned) {
			return true
		}
	}
	return false
}

func taskOutcomeMatches(p change.Payload, assigned change.TaskAssigned) bool {
	switch outcome := p.(type) {
	case change.TaskTimedOut:
		return outcome.TaskID == assigned.TaskID && outcome.Agent == assigned.Agent && outcome.Role == assigned.Role
	case change.TaskFailed:
		return outcome.TaskID == assigned.TaskID && outcome.Agent == assigned.Agent && outcome.Role == assigned.Role
	default:
		return false
	}
}

// Ack records acknowledgement of the task within the 60s window.
func (s *Service) Ack(ctx context.Context, a Assignment, at time.Time) error {
	if at.IsZero() {
		at = s.clock.Now()
	}
	if at.Sub(a.AssignedAt) > AckWindow {
		return fmt.Errorf("%w: ack at %v is after 60s window from %v", ErrAckWindowExpired, at, a.AssignedAt)
	}

	return s.appendEvents(ctx, a.ChangeID, func(existing []change.Event, _ change.State) ([]change.Event, error) {
		if err := checkTaskAgent(existing, a); err != nil {
			return nil, err
		}
		if hasTaskAcked(existing, a.TaskID) {
			return nil, nil // idempotent: second ack appends nothing
		}

		payload := change.TaskAcked{TaskID: a.TaskID, Agent: a.Agent, Role: a.Role}
		eid := s.ids.NewEvent()
		causation := lastCausation(existing)
		corr := correlationIDOf(existing, eid)
		ev, err := s.buildEventWithID(eid, a.ChangeID, int64(len(existing)+1), change.EventTaskAcked, a.Agent, corr, causation, payload, nil)
		if err != nil {
			return nil, err
		}
		return []change.Event{ev}, nil
	})
}

func validateSubmissionExpect(cid id.ChangeID, in workflow.Inputs, sub testset.Submission) error {
	testsDir := "tests/heimdall/" + string(cid)
	var acids []string
	for _, c := range in.AcceptanceCriteria {
		acids = append(acids, c.ID)
	}
	if len(acids) == 0 {
		for k := range sub.AcceptanceCriteria {
			acids = append(acids, k)
		}
		slices.Sort(acids)
	}
	if err := sub.Validate(testset.Expect{TestsDir: testsDir, ACIDs: acids}); err != nil {
		return err
	}
	return errors.Join(goSourceProblems(sub)...)
}

func hasDuplicateSubmission(events []change.Event, blobRef workflow.BlobRef) bool {
	for _, ev := range events {
		if ev.Type == change.EventTestsSubmitted && slices.Contains(ev.BlobRefs, blobRef) {
			return true
		}
	}
	return false
}

func (s *Service) buildTimedOutEvent(
	a Assignment,
	blobRef workflow.BlobRef,
	existing []change.Event,
) (change.Event, error) {
	eid := s.ids.NewEvent()
	causation := lastCausation(existing)
	corr := correlationIDOf(existing, eid)
	timedOutPayload := change.TaskTimedOut{
		TaskID: a.TaskID,
		Agent:  a.Agent,
		Role:   a.Role,
		Reason: "test submission received after deadline",
	}
	return s.buildEventWithID(eid, a.ChangeID, int64(len(existing)+1), change.EventTaskTimedOut, a.Agent, corr, causation, timedOutPayload, []workflow.BlobRef{blobRef})
}

func (s *Service) buildTestsSubmittedEvent(
	a Assignment,
	sub testset.Submission,
	blobRef workflow.BlobRef,
	existing []change.Event,
) (change.Event, error) {
	refs := make([]string, 0, len(sub.Files))
	for _, f := range sub.Files {
		refs = append(refs, f.Path)
	}
	sha := a.Task.Change.BaseSHA
	if sha == "" {
		sha = id.SHA("0000000")
	}
	payload := change.TestsSubmitted{
		Refs:     refs,
		ACMap:    sub.AcceptanceCriteria,
		SHA:      sha,
		TreeHash: sha,
	}
	eid := s.ids.NewEvent()
	causation := lastCausation(existing)
	corr := correlationIDOf(existing, eid)
	return s.buildEventWithID(eid, a.ChangeID, int64(len(existing)+1), change.EventTestsSubmitted, a.Agent, corr, causation, payload, []workflow.BlobRef{blobRef})
}

func (s *Service) storeSubmissionBlob(ctx context.Context, sub testset.Submission) (workflow.BlobRef, error) {
	rawSub, err := json.Marshal(sub)
	if err != nil {
		return "", fmt.Errorf("marshal submission: %w", err)
	}
	blobRef, err := s.blobs.Put(ctx, ports.BlobBundle, bytes.NewReader(rawSub))
	if err != nil {
		return "", fmt.Errorf("store submission blob: %w", err)
	}
	return blobRef, nil
}

// SubmitTests records test submission, checking deadline timeout semantics and idempotency.
func (s *Service) SubmitTests(ctx context.Context, a Assignment, sub testset.Submission, late bool) error {
	if a.Role != workflow.RoleTestAuthor {
		return fmt.Errorf("%w: role %s cannot submit tests", ErrWrongRole, a.Role)
	}
	if err := validateSubmissionExpect(a.ChangeID, a.Task.Inputs, sub); err != nil {
		return fmt.Errorf("%w: %w", ErrValidation, err)
	}
	blobRef, err := s.storeSubmissionBlob(ctx, sub)
	if err != nil {
		return err
	}

	isLate := late || s.clock.Now().After(a.Deadline)
	var timedOutRecorded bool

	err = s.appendEvents(ctx, a.ChangeID, func(existing []change.Event, _ change.State) ([]change.Event, error) {
		if err := checkTaskAgent(existing, a); err != nil {
			return nil, err
		}
		if hasDuplicateSubmission(existing, blobRef) {
			return nil, nil // idempotent: duplicate content appends nothing
		}
		if isLate {
			timedOutRecorded = true
			ev, err := s.buildTimedOutEvent(a, blobRef, existing)
			if err != nil {
				return nil, err
			}
			return []change.Event{ev}, nil
		}
		ev, err := s.buildTestsSubmittedEvent(a, sub, blobRef, existing)
		if err != nil {
			return nil, err
		}
		return []change.Event{ev}, nil
	})
	if err != nil {
		return err
	}
	if timedOutRecorded {
		return fmt.Errorf("%w: test submission received after deadline", ErrLateSubmission)
	}
	return nil
}

// PrepareTestsSubmission validates and builds the event the workflow engine must append with its step decisions.
// Keeping this separate from SubmitTests lets the server atomically store tests.submitted together with the
// test-quality outbox effect. Duplicate content returns found=false and remains a no-op.
func (s *Service) PrepareTestsSubmission(ctx context.Context, a Assignment, sub testset.Submission, late bool) (change.Event, bool, bool, error) {
	if a.Role != workflow.RoleTestAuthor {
		return change.Event{}, false, false, fmt.Errorf("%w: role %s cannot submit tests", ErrWrongRole, a.Role)
	}
	if err := validateSubmissionExpect(a.ChangeID, a.Task.Inputs, sub); err != nil {
		return change.Event{}, false, false, fmt.Errorf("%w: %w", ErrValidation, err)
	}
	blobRef, err := s.storeSubmissionBlob(ctx, sub)
	if err != nil {
		return change.Event{}, false, false, err
	}
	existing, err := s.events.Load(ctx, a.ChangeID)
	if err != nil {
		return change.Event{}, false, false, fmt.Errorf("load change %s: %w", a.ChangeID, err)
	}
	if err := checkTaskAgent(existing, a); err != nil {
		return change.Event{}, false, false, err
	}
	if hasDuplicateSubmission(existing, blobRef) {
		return change.Event{}, false, false, nil
	}
	isLate := late || s.clock.Now().After(a.Deadline)
	if isLate {
		ev, err := s.buildTimedOutEvent(a, blobRef, existing)
		if err != nil {
			return change.Event{}, false, false, err
		}
		if err := reducerAccepts(existing, ev); err != nil {
			return change.Event{}, false, false, err
		}
		return ev, true, true, nil
	}
	ev, err := s.buildTestsSubmittedEvent(a, sub, blobRef, existing)
	if err != nil {
		return change.Event{}, false, false, err
	}
	if err := reducerAccepts(existing, ev); err != nil {
		return change.Event{}, false, false, err
	}
	return ev, true, false, nil
}

// reducerAccepts checks that the change, as it stands, can take ev. The engine stores what it is given, so without
// this check an agent could write to a change that is already closed, and the log would then hold an event the
// reducer refuses.
func reducerAccepts(existing []change.Event, ev change.Event) error {
	state, err := change.Replay(existing)
	if err != nil {
		return fmt.Errorf("stream %s does not replay: %w", ev.ChangeID, err)
	}
	if _, err := change.Reduce(state, ev); err != nil {
		return fmt.Errorf("%w: %s refused by reducer in state %s: %w", ErrReducerRefusal, ev.Type, state, err)
	}
	return nil
}

type askedItem struct {
	qID      change.QuestionID
	senderID change.SenderID
	msgID    change.MessageID
	ref      string
}

func validateQuestions(questions []string) error {
	if len(questions) < 1 || len(questions) > 3 {
		return fmt.Errorf("%w: question count must be between 1 and 3, got %d", ErrValidation, len(questions))
	}
	for i, q := range questions {
		if strings.TrimSpace(q) == "" {
			return fmt.Errorf("%w: question %d must not be blank", ErrValidation, i+1)
		}
		if utf8.RuneCountInString(q) > 400 {
			return fmt.Errorf("%w: question %d exceeds 400 characters", ErrValidation, i+1)
		}
	}
	return nil
}

func (s *Service) postQuestions(ctx context.Context, cid id.ChangeID, questions []string) ([]askedItem, error) {
	var asked []askedItem
	for _, q := range questions {
		qID, senderID, msgID, err := s.asker.Ask(ctx, cid, q)
		if err != nil {
			return nil, fmt.Errorf("asker: %w", err)
		}
		if qID == "" {
			qID = change.QuestionID(s.ids.NewEvent())
		}
		asked = append(asked, askedItem{qID: qID, senderID: senderID, msgID: msgID, ref: q})
	}
	return asked, nil
}

func (s *Service) buildQuestionEvents(a Assignment, existing []change.Event, asked []askedItem) ([]change.Event, error) {
	var evs []change.Event
	baseVer := int64(len(existing))
	causation := lastCausation(existing)
	firstID := s.ids.NewEvent()
	corr := correlationIDOf(existing, firstID)

	// The short id of each question continues the change's count (Q1, Q2, then Q3 on a later ask), so the room and the
	// answer command name the same question.
	numbered := 0
	for _, ev := range existing {
		if ev.Type == change.EventQuestionAsked {
			numbered++
		}
	}
	for i, item := range asked {
		payload := change.QuestionAsked{
			QuestionID: item.qID,
			Refs:       []string{fmt.Sprintf("Q%d", numbered+i+1)},
			Sender:     item.senderID,
			MessageID:  item.msgID,
		}
		eid := firstID
		if i > 0 {
			eid = s.ids.NewEvent()
		}
		ev, err := s.buildEventWithID(eid, a.ChangeID, baseVer+int64(i+1), change.EventQuestionAsked, a.Agent, corr, causation, payload, nil)
		if err != nil {
			return nil, err
		}
		evs = append(evs, ev)
		causation = &ev.ID
	}
	return evs, nil
}

// AskHumans posts questions to humans via Asker and appends question.asked events.
func (s *Service) AskHumans(ctx context.Context, a Assignment, questions []string) error {
	if err := validateQuestions(questions); err != nil {
		return err
	}
	asked, err := s.postQuestions(ctx, a.ChangeID, questions)
	if err != nil {
		return err
	}

	return s.appendEvents(ctx, a.ChangeID, func(existing []change.Event, _ change.State) ([]change.Event, error) {
		if err := checkTaskAgent(existing, a); err != nil {
			return nil, err
		}
		return s.buildQuestionEvents(a, existing, asked)
	})
}

// Progress appends a durable progress report and enforces its interval from the event stream.
func (s *Service) Progress(ctx context.Context, a Assignment, text string, at time.Time) error {
	if strings.TrimSpace(text) == "" {
		return fmt.Errorf("%w: progress text must not be blank", ErrValidation)
	}
	if utf8.RuneCountInString(text) > 400 {
		return fmt.Errorf("%w: progress text must be at most 400 characters", ErrValidation)
	}
	if at.IsZero() {
		at = s.clock.Now()
	}
	return s.appendEvents(ctx, a.ChangeID, func(existing []change.Event, _ change.State) ([]change.Event, error) {
		if err := checkTaskAgent(existing, a); err != nil {
			return nil, err
		}
		lastAt, found, err := lastTaskProgress(existing, a)
		if err != nil {
			return nil, err
		}
		if found && at.Sub(lastAt) < 2*time.Minute {
			return nil, fmt.Errorf("%w: progress interval has not elapsed", ErrValidation)
		}
		ev, err := s.buildProgressEvent(a, text, at, existing)
		if err != nil {
			return nil, err
		}
		return []change.Event{ev}, nil
	})
}

func lastTaskProgress(events []change.Event, a Assignment) (time.Time, bool, error) {
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Type != change.EventTaskProgress {
			continue
		}
		p, err := change.Decode(events[i])
		if err != nil {
			return time.Time{}, false, fmt.Errorf("decode prior progress: %w", err)
		}
		progress, ok := p.(change.TaskProgress)
		if ok && progress.TaskID == a.TaskID && progress.Agent == a.Agent && progress.Role == a.Role {
			return events[i].OccurredAt, true, nil
		}
	}
	return time.Time{}, false, nil
}

func (s *Service) buildProgressEvent(a Assignment, text string, at time.Time, existing []change.Event) (change.Event, error) {
	payload := change.TaskProgress{TaskID: a.TaskID, Agent: a.Agent, Role: a.Role, Text: text}
	eid := s.ids.NewEvent()
	ev, err := s.buildEventWithID(eid, a.ChangeID, int64(len(existing)+1), change.EventTaskProgress, a.Agent, correlationIDOf(existing, eid), lastCausation(existing), payload, nil)
	if err != nil {
		return change.Event{}, err
	}
	ev.OccurredAt = at.UTC()
	return ev, nil
}

// collectAnswers pairs each question the change asked with its recorded answer, in the order asked. A question with no
// answer yet is left out (A14).
func collectAnswers(events []change.Event) []workflow.AnswerRef {
	refs := map[change.QuestionID]string{}
	var order []change.QuestionID
	answers := map[change.QuestionID]change.QuestionAnswered{}
	for _, ev := range events {
		p, err := change.Decode(ev)
		if err != nil {
			continue
		}
		switch v := p.(type) {
		case change.QuestionAsked:
			if len(v.Refs) > 0 {
				refs[v.QuestionID] = v.Refs[0]
				order = append(order, v.QuestionID)
			}
		case change.QuestionAnswered:
			answers[v.QuestionID] = v
		}
	}
	var out []workflow.AnswerRef
	for _, qid := range order {
		a, ok := answers[qid]
		if !ok {
			continue
		}
		out = append(out, workflow.AnswerRef{ID: refs[qid], Answer: a.Answer, AnsweredBy: string(a.Sender)})
	}
	return out
}
