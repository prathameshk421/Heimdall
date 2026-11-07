package agenttools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/deliberation"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
)

type validFinding struct {
	input finding.Input
	tier  finding.Tier
}

func (s *Service) buildFindingFiledEvent(
	a Assignment,
	vf validFinding,
	fRef id.FindingRef,
	version int64,
	eid change.EventID,
	corr change.CorrelationID,
	causation *change.EventID,
) (change.Event, error) {
	st := finding.StateOpen
	if vf.tier == finding.TierE4 {
		st = finding.StateUnverified
	}
	payload := change.FindingFiled{
		FindingID:   s.ids.NewFinding(),
		Ref:         fRef,
		Tier:        vf.tier,
		Severity:    vf.input.Severity,
		State:       st,
		By:          change.ActorID(a.Agent),
		Fingerprint: findingFingerprint(vf.input),
		Confidence:  vf.input.Confidence,
		Claim:       vf.input.Claim,
		Category:    vf.input.Category,
		Location:    &vf.input.Location,
	}
	payload.ReproTest, payload.ToolResult, payload.MemoryRef = proofsOf(vf)
	payload.CardRefs = cardRefsOf(vf.input)
	return s.buildEventWithID(eid, a.ChangeID, version, change.EventFindingFiled, a.Agent, corr, causation, payload, nil)
}

// proofsOf returns the evidence the server repeats to verify the finding's claimed tier: the repro test of an E1
// finding, the tool result of an E2 finding, the memory reference of an E3 finding. An E4 finding carries none.
func proofsOf(vf validFinding) (*finding.ReproTest, *finding.ToolResult, *string) {
	for _, e := range vf.input.Evidence {
		switch {
		case vf.tier == finding.TierE1 && e.Type == finding.EvidenceReproTest && e.ReproTest != nil:
			return e.ReproTest, nil, nil
		case vf.tier == finding.TierE2 && e.Type == finding.EvidenceToolResult && e.ToolResult != nil:
			return nil, e.ToolResult, nil
		case vf.tier == finding.TierE3 && e.Type == finding.EvidenceMemory && e.MemoryRef != nil:
			return nil, nil, e.MemoryRef
		}
	}
	return nil, nil, nil
}

// cardRefsOf returns the rule card file names a finding used: in context_refs, or cited as its memory evidence.
func cardRefsOf(input finding.Input) []string {
	var out []string
	add := func(name string) {
		if name != "" && !slices.Contains(out, name) {
			out = append(out, name)
		}
	}
	for _, r := range input.ContextRefs {
		if name, ok := finding.CardRef(r); ok {
			add(name)
		}
	}
	for _, e := range input.Evidence {
		if e.Type == finding.EvidenceMemory && e.MemoryRef != nil {
			if name, _, ok := finding.CardEvidence(*e.MemoryRef); ok {
				add(name)
			}
		}
	}
	return out
}

func findingFingerprint(input finding.Input) string {
	location := strings.ToLower(strings.TrimSpace(input.Location.File))
	claim := strings.ToLower(strings.Join(strings.Fields(input.Claim), " "))
	sum := sha256.Sum256([]byte(string(input.Category) + "\n" + location + ":" + fmt.Sprint(input.Location.Lines[0], "-", input.Location.Lines[1]) + "\n" + claim))
	return hex.EncodeToString(sum[:])
}

func (s *Service) buildReviewEvents(
	a Assignment,
	existing []change.Event,
	valids []validFinding,
	ruleChecks []finding.RuleCheck,
) ([]change.Event, error) {
	findingCount := 0
	for _, ev := range existing {
		if ev.Type == change.EventFindingFiled {
			findingCount++
		}
	}

	var evs []change.Event
	var filedRefs []id.FindingRef
	baseVer := int64(len(existing))
	causation := lastCausation(existing)
	firstID := s.ids.NewEvent()
	corr := correlationIDOf(existing, firstID)

	for i, vf := range valids {
		findingCount++
		fRef, err := id.ParseFindingRef(fmt.Sprintf("F%d", findingCount))
		if err != nil {
			return nil, err
		}
		filedRefs = append(filedRefs, fRef)

		eid := firstID
		if i > 0 {
			eid = s.ids.NewEvent()
		}
		ev, err := s.buildFindingFiledEvent(a, vf, fRef, baseVer+int64(len(evs)+1), eid, corr, causation)
		if err != nil {
			return nil, err
		}
		evs = append(evs, ev)
		causation = &ev.ID
	}

	reviewPayload := change.ReviewSubmitted{
		Reviewer:    a.Agent,
		FindingRefs: filedRefs,
		PackDate:    s.clock.Now().UTC().Format("2006-01-02"),
		RuleChecks:  ruleChecks,
	}
	revEv, err := s.buildEventWithID(s.ids.NewEvent(), a.ChangeID, baseVer+int64(len(evs)+1), change.EventReviewSubmitted, a.Agent, corr, causation, reviewPayload, nil)
	if err != nil {
		return nil, err
	}
	evs = append(evs, revEv)
	return evs, nil
}

// SubmitReview validates findings and records review.submitted and finding.filed events.
func (s *Service) SubmitReview(ctx context.Context, a Assignment, r finding.Review, _ bool) (Receipt, error) {
	if err := validateReviewAssignment(a, r); err != nil {
		return Receipt{}, err
	}

	findingErrors, valids, err := s.validateReview(ctx, a, r)
	if err != nil {
		return Receipt{}, err
	}
	if len(r.Findings) > 0 && len(valids) == 0 && len(findingErrors) > 0 {
		return Receipt{Accepted: false, FindingErrors: findingErrors}, nil
	}

	err = s.appendEventsWith(ctx, a.ChangeID, func(existing []change.Event, _ change.State) ([]change.Event, error) {
		if err := checkTaskAgent(existing, a); err != nil {
			return nil, err
		}
		if reviewAlreadySubmittedForTask(existing, a.TaskID, a.Agent) {
			return nil, nil
		}
		return s.buildReviewEvents(a, existing, valids, r.RuleChecks)
	}, s.advance)
	if err != nil {
		return Receipt{}, err
	}

	accepted := len(findingErrors) == 0
	return Receipt{Accepted: accepted, FindingErrors: findingErrors}, nil
}

func validateReviewAssignment(a Assignment, r finding.Review) error {
	if a.Role != workflow.RoleReviewer {
		return fmt.Errorf("%w: role %s cannot submit review", ErrWrongRole, a.Role)
	}
	if a.Agent == "" {
		return ErrWrongAgent
	}
	if a.Task.Change.HeadSHA != "" && r.HeadSHA != "" && a.Task.Change.HeadSHA != r.HeadSHA {
		return fmt.Errorf("%w: head_sha does not match task head", ErrValidation)
	}
	return nil
}

func reviewAlreadySubmittedForTask(events []change.Event, taskID id.TaskID, agent id.AgentName) bool {
	assignedAt := assignedTaskEventIndex(events, taskID, agent)
	return assignedAt >= 0 && hasReviewSubmissionAfter(events, assignedAt, agent)
}

func assignedTaskEventIndex(events []change.Event, taskID id.TaskID, agent id.AgentName) int {
	assignedAt := -1
	for i, ev := range events {
		if ev.Type != change.EventTaskAssigned {
			continue
		}
		payload, err := change.Decode(ev)
		assigned, ok := payload.(change.TaskAssigned)
		if err == nil && ok && assigned.TaskID == taskID && assigned.Agent == agent && assigned.Role == workflow.RoleReviewer {
			assignedAt = i
		}
	}
	return assignedAt
}

func hasReviewSubmissionAfter(events []change.Event, assignedAt int, agent id.AgentName) bool {
	for _, ev := range events[assignedAt+1:] {
		if ev.Type != change.EventReviewSubmitted {
			continue
		}
		payload, err := change.Decode(ev)
		submitted, ok := payload.(change.ReviewSubmitted)
		if err == nil && ok && submitted.Reviewer == agent {
			return true
		}
	}
	return false
}

func (s *Service) validateReview(ctx context.Context, a Assignment, r finding.Review) ([]FindingError, []validFinding, error) {
	var findingErrors []FindingError
	var valids []validFinding
	for i, input := range r.Findings {
		accepted, problems, err := s.validateReviewFinding(ctx, a, r, i, input)
		if err != nil {
			return nil, nil, err
		}
		findingErrors = append(findingErrors, problems...)
		valids = append(valids, accepted...)
	}
	if len(r.Findings) == 0 {
		result, err := s.review.Submit(ctx, ReviewSubmission{Change: a.Task.Change, Caller: a.Agent, FilesChanged: a.Task.Inputs.FilesChanged, Review: r})
		if err != nil {
			return nil, nil, fmt.Errorf("validate review: %w", err)
		}
		for _, problem := range result.Problems {
			findingErrors = append(findingErrors, FindingError{Index: -1, Message: problem.Message})
		}
	}
	return findingErrors, valids, nil
}

func (s *Service) validateReviewFinding(ctx context.Context, a Assignment, r finding.Review, index int, input finding.Input) ([]validFinding, []FindingError, error) {
	submission := ReviewSubmission{Change: a.Task.Change, Caller: a.Agent, FilesChanged: a.Task.Inputs.FilesChanged, Review: r}
	submission.Review.Clean = false
	submission.Review.Findings = []finding.Input{input}
	result, err := s.review.Submit(ctx, submission)
	if err != nil {
		return nil, nil, fmt.Errorf("validate review finding %d: %w", index, err)
	}
	problems := make([]FindingError, 0, len(result.Problems))
	for _, problem := range result.Problems {
		problems = append(problems, FindingError{Index: index, Message: problem.Message})
	}
	if len(problems) > 0 {
		return nil, problems, nil
	}
	valids := make([]validFinding, 0, len(result.Findings))
	for _, accepted := range result.Findings {
		valids = append(valids, validFinding{input: accepted.Input, tier: accepted.Tier})
	}
	return valids, nil, nil
}

// SubmitVerdict records resolver verdict for a case.
func (s *Service) SubmitVerdict(ctx context.Context, a Assignment, v deliberation.Verdict, _ bool) error {
	if a.Role != workflow.RoleResolver {
		return fmt.Errorf("%w: role %s cannot submit verdict", ErrWrongRole, a.Role)
	}
	if a.Agent == "" {
		return ErrWrongAgent
	}
	if err := v.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrValidation, err)
	}
	if a.Task.CaseID == "" || a.Task.CaseID != v.CaseID {
		return fmt.Errorf("%w: case_id does not match the assigned resolver case", ErrValidation)
	}

	return s.appendEvents(ctx, a.ChangeID, func(existing []change.Event, _ change.State) ([]change.Event, error) {
		if err := checkTaskAgent(existing, a); err != nil {
			return nil, err
		}

		citations := make([]string, 0, len(v.Citations))
		for _, c := range v.Citations {
			citations = append(citations, c.Ref)
		}
		payload := change.VerdictSubmitted{
			CaseID:    v.CaseID,
			Verdict:   change.Verdict(v.Decision),
			Citations: citations,
			Checks:    []string{},
		}
		eid := s.ids.NewEvent()
		causation := lastCausation(existing)
		corr := correlationIDOf(existing, eid)
		ev, err := s.buildEventWithID(eid, a.ChangeID, int64(len(existing)+1), change.EventVerdictSubmitted, a.Agent, corr, causation, payload, nil)
		if err != nil {
			return nil, err
		}
		return []change.Event{ev}, nil
	})
}
