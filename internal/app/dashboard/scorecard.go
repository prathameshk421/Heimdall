package dashboard

import (
	"context"
	"fmt"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

// RoleScorecard summarizes quality metrics per agent role, seat, card and category computed purely from the event log (R6, L6).
type RoleScorecard struct {
	TestAuthor TestAuthorScore         `json:"test_author"`
	Reviewer   ReviewerScore           `json:"reviewer"`
	BySeat     map[string]ReviewerSeat `json:"by_seat,omitempty"`
	ByCard     map[string]CardScore    `json:"by_card,omitempty"`
	ByCategory map[string]CardScore    `json:"by_category,omitempty"`
	Resolver   ResolverScore           `json:"resolver"`
	Profiler   ProfilerScore           `json:"profiler"`
}

// CardScore tracks finding outcomes for one rule card or finding category (L6).
type CardScore struct {
	Filed     int `json:"filed"`
	Verified  int `json:"verified"`
	Upheld    int `json:"upheld"`
	Dismissed int `json:"dismissed"`
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

type findingMeta struct {
	owner    string
	category string
	cards    []string
}

// ScorecardFromEvents computes the per-role, per-card and per-category quality scorecard from a slice of events (R6, L6).
func ScorecardFromEvents(events []change.Event) RoleScorecard {
	sc := RoleScorecard{
		BySeat:     map[string]ReviewerSeat{},
		ByCard:     map[string]CardScore{},
		ByCategory: map[string]CardScore{},
	}
	meta := map[id.FindingID]findingMeta{}
	for _, ev := range events {
		p, err := change.Decode(ev)
		if err != nil {
			continue
		}
		applyScorecardEvent(&sc, meta, p)
	}
	if sc.TestAuthor.Submitted > 0 {
		sc.TestAuthor.RedFirstRate = float64(sc.TestAuthor.Verified) / float64(sc.TestAuthor.Submitted)
	}
	return sc
}

func applyScorecardEvent(sc *RoleScorecard, meta map[id.FindingID]findingMeta, p change.Payload) {
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
		applyScorecardFinding(sc, meta, p)
	default:
		applyScorecardOther(sc, p)
	}
}

func applyScorecardFinding(sc *RoleScorecard, meta map[id.FindingID]findingMeta, p change.Payload) {
	switch v := p.(type) {
	case change.FindingFiled:
		sc.Reviewer.Filed++
		m := findingMeta{owner: string(v.By), category: string(v.Category), cards: v.CardRefs}
		meta[v.FindingID] = m
		seat := sc.BySeat[m.owner]
		seat.Filed++
		sc.BySeat[m.owner] = seat
		bumpCardScore(sc, m, func(cs *CardScore) { cs.Filed++ })
	case change.FindingVerified:
		sc.Reviewer.Verified++
		m := meta[v.FindingID]
		if m.owner != "" {
			seat := sc.BySeat[m.owner]
			seat.Verified++
			sc.BySeat[m.owner] = seat
		}
		bumpCardScore(sc, m, func(cs *CardScore) { cs.Verified++ })
	case change.FindingStateChanged:
		applyFindingOutcome(sc, meta[v.FindingID], v.State)
	}
}

func bumpCardScore(sc *RoleScorecard, m findingMeta, fn func(*CardScore)) {
	if m.category != "" {
		cs := sc.ByCategory[m.category]
		fn(&cs)
		sc.ByCategory[m.category] = cs
	}
	for _, card := range m.cards {
		cs := sc.ByCard[card]
		fn(&cs)
		sc.ByCard[card] = cs
	}
}

func applyFindingOutcome(sc *RoleScorecard, m findingMeta, st finding.State) {
	switch st {
	case finding.StateUpheld:
		sc.Reviewer.Upheld++
		if m.owner != "" {
			seat := sc.BySeat[m.owner]
			seat.Upheld++
			sc.BySeat[m.owner] = seat
		}
		bumpCardScore(sc, m, func(cs *CardScore) { cs.Upheld++ })
	case finding.StateDismissed:
		sc.Reviewer.Dismissed++
		if m.owner != "" {
			seat := sc.BySeat[m.owner]
			seat.Dismissed++
			sc.BySeat[m.owner] = seat
		}
		bumpCardScore(sc, m, func(cs *CardScore) { cs.Dismissed++ })
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
