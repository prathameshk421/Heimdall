package agenttools

import (
	"context"
	"errors"
	"fmt"
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

func validateReviewFindings(findings []finding.Input) ([]FindingError, []validFinding) {
	var findingErrors []FindingError
	var valids []validFinding
	for i, f := range findings {
		if err := validateFindingShape(f); err != nil {
			findingErrors = append(findingErrors, FindingError{
				Index:   i,
				Message: err.Error(),
			})
			continue
		}
		valids = append(valids, validFinding{
			input: f,
			tier:  tierForFinding(f),
		})
	}
	return findingErrors, valids
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
		FindingID: s.ids.NewFinding(),
		Ref:       fRef,
		Tier:      vf.tier,
		State:     st,
		By:        change.ActorID(a.Agent),
	}
	return s.buildEventWithID(eid, a.ChangeID, version, change.EventFindingFiled, a.Agent, corr, causation, payload, nil)
}

func (s *Service) buildReviewEvents(
	a Assignment,
	existing []change.Event,
	valids []validFinding,
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
	if a.Role != workflow.RoleReviewer {
		return Receipt{}, fmt.Errorf("%w: role %s cannot submit review", ErrWrongRole, a.Role)
	}
	if a.Agent == "" {
		return Receipt{}, ErrWrongAgent
	}
	if a.Task.Change.HeadSHA != "" && r.HeadSHA != "" && a.Task.Change.HeadSHA != r.HeadSHA {
		return Receipt{}, fmt.Errorf("%w: head_sha does not match task head", ErrValidation)
	}

	findingErrors, valids := validateReviewFindings(r.Findings)
	if len(r.Findings) > 0 && len(valids) == 0 {
		return Receipt{Accepted: false, FindingErrors: findingErrors}, nil
	}

	err := s.appendEvents(ctx, a.ChangeID, func(existing []change.Event, _ change.State) ([]change.Event, error) {
		if err := checkTaskAgent(existing, a); err != nil {
			return nil, err
		}
		return s.buildReviewEvents(a, existing, valids)
	})
	if err != nil {
		return Receipt{}, err
	}

	accepted := len(findingErrors) == 0
	return Receipt{Accepted: accepted, FindingErrors: findingErrors}, nil
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

func validateFindingShape(f finding.Input) error {
	if err := finding.CheckRepoPath(f.Location.File); err != nil {
		return fmt.Errorf("location.file: %w", err)
	}
	first, last := f.Location.Lines[0], f.Location.Lines[1]
	if first < 1 || last < first {
		return fmt.Errorf("location.lines must be 1-based with first <= last, got [%d, %d]", first, last)
	}
	if len(f.Evidence) == 0 {
		return errors.New("evidence needs at least 1 entry")
	}
	if strings.TrimSpace(f.Claim) == "" {
		return errors.New("claim must not be blank")
	}
	return nil
}

func tierForEvidence(et finding.EvidenceType) finding.Tier {
	switch et {
	case finding.EvidenceReproTest:
		return finding.TierE1
	case finding.EvidenceToolResult:
		return finding.TierE2
	case finding.EvidenceMemory:
		return finding.TierE3
	case finding.EvidencePackRef, finding.EvidenceReasoning:
		return finding.TierE4
	default:
		return finding.TierE4
	}
}

func tierForFinding(f finding.Input) finding.Tier {
	best := finding.TierE4
	for _, ev := range f.Evidence {
		t := tierForEvidence(ev.Type)
		if t.AtLeast(best) {
			best = t
		}
	}
	return best
}
