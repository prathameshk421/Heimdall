package dashboard

import (
	"context"
	"fmt"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

// RoleScorecard summarizes quality metrics per agent role computed purely from the event log (R6).
type RoleScorecard struct {
	TestAuthor TestAuthorScore         `json:"test_author"`
	Reviewer   ReviewerScore           `json:"reviewer"`
	BySeat     map[string]ReviewerSeat `json:"by_seat,omitempty"`
	Resolver   ResolverScore           `json:"resolver"`
	Profiler   ProfilerScore           `json:"profiler"`
}

// TestAuthorScore tracks test submissions, red-first verifications, rejections, and commits.
type TestAuthorScore struct {
	Submitted    int     `json:"submitted"`
	Verified     int     `json:"verified"`
	Rejected     int     `json:"rejected"`
	Committed    int     `json:"committed"`
	RedFirstRate float64 `json:"red_first_rate"`
}

// ReviewerScore tracks reviews and finding outcomes across all reviewer seats.
type ReviewerScore struct {
	Reviews   int `json:"reviews"`
	Filed     int `json:"filed"`
	Verified  int `json:"verified"`
	Upheld    int `json:"upheld"`
	Dismissed int `json:"dismissed"`
	Resolved  int `json:"resolved"`
	Failed    int `json:"tasks_failed"`
}

// ReviewerSeat tracks quality counts for one named reviewer seat.
type ReviewerSeat struct {
	Reviews   int `json:"reviews"`
	Filed     int `json:"filed"`
	Verified  int `json:"verified"`
	Upheld    int `json:"upheld"`
	Dismissed int `json:"dismissed"`
}

// ResolverScore tracks deliberation cases and verdicts.
type ResolverScore struct {
	CasesOpened int `json:"cases_opened"`
	Submitted   int `json:"verdicts_submitted"`
	Applied     int `json:"verdicts_applied"`
	Escalated   int `json:"verdicts_escalated"`
}

// ProfilerScore tracks profile requests and proposals.
type ProfilerScore struct {
	Requests  int `json:"requests"`
	Proposals int `json:"proposals"`
}

// Scorecard computes the per-role quality scorecard across all persisted changes (R6).
func (s *Service) Scorecard(ctx context.Context) (RoleScorecard, error) {
	ids, err := s.history.ChangeIDs(ctx)
	if err != nil {
		return RoleScorecard{}, fmt.Errorf("scorecard: list changes: %w", err)
	}
	var all []change.Event
	for _, cid := range ids {
		evs, err := s.history.Load(ctx, cid)
		if err != nil {
			return RoleScorecard{}, fmt.Errorf("scorecard: load %s: %w", cid, err)
		}
		all = append(all, evs...)
	}
	return ScorecardFromEvents(all), nil
}

// ScorecardFromEvents computes the per-role quality scorecard from a slice of events (R6).
func ScorecardFromEvents(events []change.Event) RoleScorecard {
	sc := RoleScorecard{BySeat: map[string]ReviewerSeat{}}
	owner := map[id.FindingID]string{}
	for _, ev := range events {
		p, err := change.Decode(ev)
		if err != nil {
			continue
		}
		applyScorecardEvent(&sc, owner, p)
	}
	if sc.TestAuthor.Submitted > 0 {
		sc.TestAuthor.RedFirstRate = float64(sc.TestAuthor.Verified) / float64(sc.TestAuthor.Submitted)
	}
	return sc
}

func applyScorecardEvent(sc *RoleScorecard, owner map[id.FindingID]string, p change.Payload) {
	switch v := p.(type) {
	case change.TestsSubmitted:
		sc.TestAuthor.Submitted++
	case change.TestsVerified:
		sc.TestAuthor.Verified++
	case change.TestsRejected:
		sc.TestAuthor.Rejected++
	case change.TestsCommitted:
		sc.TestAuthor.Committed++
	case change.ReviewSubmitted:
		sc.Reviewer.Reviews++
		seat := sc.BySeat[string(v.Reviewer)]
		seat.Reviews++
		sc.BySeat[string(v.Reviewer)] = seat
	case change.TaskFailed:
		if v.Role == "reviewer" {
			sc.Reviewer.Failed++
		}
	case change.FindingFiled, change.FindingVerified, change.FindingStateChanged:
		applyScorecardFinding(sc, owner, p)
	default:
		applyScorecardOther(sc, p)
	}
}

func applyScorecardFinding(sc *RoleScorecard, owner map[id.FindingID]string, p change.Payload) {
	switch v := p.(type) {
	case change.FindingFiled:
		sc.Reviewer.Filed++
		who := string(v.By)
		owner[v.FindingID] = who
		seat := sc.BySeat[who]
		seat.Filed++
		sc.BySeat[who] = seat
	case change.FindingVerified:
		sc.Reviewer.Verified++
		if who := owner[v.FindingID]; who != "" {
			seat := sc.BySeat[who]
			seat.Verified++
			sc.BySeat[who] = seat
		}
	case change.FindingStateChanged:
		applyFindingOutcome(sc, owner[v.FindingID], v.State)
	}
}

func applyFindingOutcome(sc *RoleScorecard, who string, st finding.State) {
	switch st {
	case finding.StateUpheld:
		sc.Reviewer.Upheld++
		if who != "" {
			seat := sc.BySeat[who]
			seat.Upheld++
			sc.BySeat[who] = seat
		}
	case finding.StateDismissed:
		sc.Reviewer.Dismissed++
		if who != "" {
			seat := sc.BySeat[who]
			seat.Dismissed++
			sc.BySeat[who] = seat
		}
	case finding.StateResolved:
		sc.Reviewer.Resolved++
	case finding.StateProposed, finding.StateOpen, finding.StateUnverified, finding.StateDisputed,
		finding.StateWithdrawn, finding.StateEscalated, finding.StateSpecChanged, finding.StateFixClaimed, finding.StateVerifying:
	}
}

func applyScorecardOther(sc *RoleScorecard, p change.Payload) {
	switch p.(type) {
	case change.CaseOpened:
		sc.Resolver.CasesOpened++
	case change.VerdictSubmitted:
		sc.Resolver.Submitted++
	case change.VerdictApplied:
		sc.Resolver.Applied++
	case change.VerdictEscalated:
		sc.Resolver.Escalated++
	case change.ProfileRequested:
		sc.Profiler.Requests++
	case change.ProfileProposed:
		sc.Profiler.Proposals++
	}
}
