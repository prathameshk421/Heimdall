package gateuse

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/gate"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// Config holds the ports an Evaluator needs. Every field is required.
type Config struct {
	Events ports.EventStore
	Source Source
	Clock  ports.Clock
	IDs    ports.IDs
}

// Evaluator evaluates the merge rule and publishes its check.
type Evaluator struct {
	events ports.EventStore
	source Source
	clock  ports.Clock
	ids    ports.IDs
}

// Result says what one Evaluate call decided and wrote.
type Result struct {
	ChangeID id.ChangeID
	Appended bool
	Version  int64
	OK       bool
	Reasons  []string
	Status   ports.CheckStatus
}

// New checks the configuration and builds an Evaluator.
func New(cfg Config) (*Evaluator, error) {
	var missing []string
	if cfg.Events == nil {
		missing = append(missing, "Events")
	}
	if cfg.Source == nil {
		missing = append(missing, "Source")
	}
	if cfg.Clock == nil {
		missing = append(missing, "Clock")
	}
	if cfg.IDs == nil {
		missing = append(missing, "IDs")
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("gateuse: missing required config fields: %s", strings.Join(missing, ", "))
	}
	return &Evaluator{
		events: cfg.Events,
		source: cfg.Source,
		clock:  cfg.Clock,
		ids:    cfg.IDs,
	}, nil
}

const maxRetries = 5

// Evaluate folds the stream, applies the merge rule, and appends one gate.evaluated event with its github_check
// effect when the evaluation changed.
func (e *Evaluator) Evaluate(ctx context.Context, changeID id.ChangeID) (Result, error) {
	for attempt := 0; attempt <= maxRetries; attempt++ {
		res, retriable, err := e.tryEvaluate(ctx, changeID)
		if err == nil {
			return res, nil
		}
		if !retriable {
			return Result{}, err
		}
	}
	return Result{}, ports.ErrVersionConflict
}

func (e *Evaluator) loadAndFold(ctx context.Context, changeID id.ChangeID) ([]change.Event, folded, error) {
	events, err := e.events.Load(ctx, changeID)
	if err != nil {
		return nil, folded{}, fmt.Errorf("load change %s: %w", changeID, err)
	}
	f, err := foldEvents(events)
	if err != nil {
		return nil, folded{}, err
	}
	if f.closed {
		return nil, folded{}, ErrPullRequestClosed
	}
	return events, f, nil
}

func (e *Evaluator) evaluateGate(ctx context.Context, changeID id.ChangeID, f folded) (Outside, gate.Facts, gate.Decision, error) {
	approvers := make([]string, 0, len(f.facts.Approvals))
	for _, a := range f.facts.Approvals {
		if a.Approved && a.Commit == f.facts.HeadSHA {
			approvers = append(approvers, a.Login)
		}
	}
	outside, err := e.source.Outside(ctx, Request{
		ChangeID:  changeID,
		Repo:      f.repo,
		Number:    f.number,
		HeadSHA:   f.facts.HeadSHA,
		Author:    change.ActorID(f.facts.AuthorLogin),
		Approvers: approvers,
	})
	if err != nil {
		return Outside{}, gate.Facts{}, gate.Decision{}, err
	}
	facts := applyOutside(f.facts, outside)
	consensusStates := withConsensus(f.states, outside.PolicyAtBase.Consensus)
	facts.Blocking = gate.BlockingSet(consensusStates, blockingFor(outside))
	return outside, facts, gate.Evaluate(facts), nil
}

func withConsensus(states []gate.FindingState, rules policy.Consensus) []gate.FindingState {
	reporters := highConfidenceReporters(states)
	out := append([]gate.FindingState(nil), states...)
	for i := range out {
		if out[i].Fingerprint == "" {
			continue
		}
		out[i].Consensus = reporters[out[i].Fingerprint] >= rules.MinHighReporters
	}
	return out
}

// highConfidenceReporters counts the distinct reporters who still hold each fingerprint at high confidence. Only a
// live report counts: a dismissed, withdrawn or resolved report is a rejected claim, not a second opinion. This is the
// build rule of TECH_SPEC 11.2b (D40); the weighted score is Later and must not decide a hold today.
func highConfidenceReporters(states []gate.FindingState) map[string]int {
	counts := make(map[string]map[string]bool)
	for _, state := range states {
		if state.Fingerprint == "" || state.Reporter == "" || state.Confidence != finding.ConfidenceHigh || !state.State.CanBlock() {
			continue
		}
		if counts[state.Fingerprint] == nil {
			counts[state.Fingerprint] = make(map[string]bool)
		}
		counts[state.Fingerprint][state.Reporter] = true
	}
	out := make(map[string]int, len(counts))
	for fingerprint, reporters := range counts {
		out[fingerprint] = len(reporters)
	}
	return out
}

func (e *Evaluator) tryEvaluate(ctx context.Context, changeID id.ChangeID) (Result, bool, error) {
	events, f, err := e.loadAndFold(ctx, changeID)
	if err != nil {
		return Result{}, false, err
	}

	outside, facts, decision, err := e.evaluateGate(ctx, changeID, f)
	if err != nil {
		return Result{}, false, err
	}

	status, conclusion := decideStatus(decision.OK, f.pending())
	reasons := buildReasons(decision.Unmet)

	lastGE := latestGateEvaluated(events)
	if isIdenticalEvaluation(lastGE, f.facts.HeadSHA, decision.OK, reasons) {
		return Result{
			ChangeID: changeID,
			Appended: false,
			Version:  int64(len(events)),
			OK:       decision.OK,
			Reasons:  reasons,
			Status:   status,
		}, false, nil
	}

	summary := buildSummary(decision.OK, f.pending(), reasons)
	expectedVersion := int64(len(events))
	newVersion := expectedVersion + 1

	ev, err := e.buildGateEvent(changeID, f, outside, facts, decision.OK, reasons, newVersion)
	if err != nil {
		return Result{}, false, err
	}

	fx, err := buildCheckEffect(changeID, f.repo, f.facts.HeadSHA, policyVersion(outside), status, conclusion, summary, newVersion)
	if err != nil {
		return Result{}, false, err
	}

	ver, err := e.events.Append(ctx, changeID, expectedVersion, []change.Event{ev}, []change.Effect{fx})
	if err != nil {
		return Result{}, errors.Is(err, ports.ErrVersionConflict), fmt.Errorf("append gate.evaluated: %w", err)
	}

	return Result{
		ChangeID: changeID,
		Appended: true,
		Version:  ver,
		OK:       decision.OK,
		Reasons:  reasons,
		Status:   status,
	}, false, nil
}

func decideStatus(ok bool, pending bool) (ports.CheckStatus, change.CIConclusion) {
	switch {
	case ok:
		return ports.CheckCompleted, change.CISuccess
	case pending:
		return ports.CheckInProgress, ""
	default:
		return ports.CheckCompleted, change.CIFailure
	}
}

func buildReasons(unmet []gate.Clause) []string {
	if len(unmet) == 0 {
		return nil
	}
	reasons := make([]string, 0, len(unmet))
	for _, u := range unmet {
		reasons = append(reasons, clip(fmt.Sprintf("%s: %s", u.ID, u.Reason), 256))
	}
	return reasons
}

func buildSummary(ok bool, pending bool, reasons []string) string {
	switch {
	case ok:
		return "the merge rule passed"
	case pending:
		return "the merge rule is pending"
	default:
		return clip(strings.Join(reasons, "\n"), 2000)
	}
}

func latestGateEvaluated(events []change.Event) *change.GateEvaluated {
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Type == change.EventGateEvaluated {
			p, err := change.Decode(events[i])
			if err == nil {
				if ge, ok := p.(change.GateEvaluated); ok {
					return &ge
				}
			}
		}
	}
	return nil
}

func isIdenticalEvaluation(last *change.GateEvaluated, head id.SHA, ok bool, reasons []string) bool {
	return last != nil && last.SHA == head && last.OK == ok && slices.Equal(last.Reasons, reasons)
}

func (e *Evaluator) buildGateEvent(
	changeID id.ChangeID,
	f folded,
	outside Outside,
	facts gate.Facts,
	ok bool,
	reasons []string,
	newVersion int64,
) (change.Event, error) {
	polVer := policyVersion(outside)
	treeHash := eventTreeHash(facts)

	ge := change.GateEvaluated{
		OK:            ok,
		Reasons:       reasons,
		SHA:           f.facts.HeadSHA,
		TreeHash:      treeHash,
		PolicyVersion: polVer,
	}
	if err := ge.Validate(); err != nil {
		return change.Event{}, fmt.Errorf("gate.evaluated payload invalid: %w", err)
	}
	rawEvent, err := json.Marshal(ge)
	if err != nil {
		return change.Event{}, fmt.Errorf("marshal gate.evaluated: %w", err)
	}

	eid := e.ids.NewEvent()
	ev := change.Event{
		ID:            eid,
		ChangeID:      changeID,
		Version:       newVersion,
		Type:          change.EventGateEvaluated,
		Actor:         change.Actor{Kind: change.ActorSystem, ID: "engine"},
		OccurredAt:    e.clock.Now().UTC(),
		CorrelationID: change.CorrelationID(eid.String()),
		Payload:       rawEvent,
	}
	if err := ev.Validate(); err != nil {
		return change.Event{}, fmt.Errorf("gate.evaluated event invalid: %w", err)
	}
	return ev, nil
}

func buildCheckEffect(
	changeID id.ChangeID,
	repo change.Repo,
	head id.SHA,
	polVer int,
	status ports.CheckStatus,
	conclusion change.CIConclusion,
	summary string,
	newVersion int64,
) (change.Effect, error) {
	fxDiscriminator := fmt.Sprintf("%s:%s:%d", repo, head, polVer)
	fxKey, err := change.IdempotencyKey(changeID, newVersion, change.EffectGitHubCheck, fxDiscriminator)
	if err != nil {
		return change.Effect{}, fmt.Errorf("idempotency key: %w", err)
	}

	checkPayload := change.CheckPayload{
		Repo:       repo,
		SHA:        head,
		Name:       "heimdall/gate",
		Status:     change.CheckStatus(status),
		Conclusion: conclusion,
		Summary:    clip(summary, change.MaxCheckSummary),
	}
	if err := checkPayload.Validate(); err != nil {
		return change.Effect{}, fmt.Errorf("github_check payload invalid: %w", err)
	}
	rawFx, err := json.Marshal(checkPayload)
	if err != nil {
		return change.Effect{}, fmt.Errorf("marshal github_check payload: %w", err)
	}

	fx := change.Effect{
		Key:     fxKey,
		Kind:    change.EffectGitHubCheck,
		Target:  string(repo),
		Payload: rawFx,
	}
	if err := fx.Validate(); err != nil {
		return change.Effect{}, fmt.Errorf("github_check effect invalid: %w", err)
	}

	return fx, nil
}
