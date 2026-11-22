package action

import (
	"maps"
	"slices"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/proof"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/summary"
)

// summaryVersion counts the events the summary reads, so writing the summary does not make it stale. The
// summary.posted events are left out for exactly that reason (ACTION_MODE 2.5).
func summaryVersion(events []change.Event) int {
	n := 0
	for _, e := range events {
		if e.Type != change.EventSummaryPosted {
			n++
		}
	}
	return n
}

// headAt is when the current head was seen: the time of the last pr.opened or pr.pushed event. The sweep row reads the
// age from it, so a log with neither gives no age and the sweep waits.
func headAt(events []change.Event) time.Time {
	var at time.Time
	for _, e := range events {
		if e.Type == change.EventPROpened || e.Type == change.EventPRPushed {
			at = e.OccurredAt
		}
	}
	return at
}

// oursOf lists Heimdall's own findings for the duplicate rule. The fold keeps a finding's claim but not its place, so
// the place is read from the finding.filed event that carried it. A finding whose event is gone, or that has no place
// or no claim, matches no comment.
func oursOf(events []change.Event) []OurFinding {
	out := make([]OurFinding, 0, len(events))
	for _, e := range events {
		if e.Type != change.EventFindingFiled {
			continue
		}
		p, err := change.Decode(e)
		if err != nil {
			continue
		}
		rec, ok := p.(change.FindingFiled)
		if !ok || rec.Location == nil || rec.Claim == "" {
			continue
		}
		out = append(out, OurFinding{Ref: rec.Ref.String(), Location: *rec.Location, Claim: rec.Claim})
	}
	return out
}

// summaryFindings turns the view's findings into the summary's list, in ref order, with the proof result the log holds
// for the current head. Every finding in an Action-mode log is ours: a third-party comment is a candidate under
// external.ingested until the proof run files it as a finding (AM14b).
func summaryFindings(events []change.Event, v change.View, head id.SHA) []summary.Finding {
	places := placesOf(events)
	refs := slices.Sorted(maps.Keys(v.Findings))
	out := make([]summary.Finding, 0, len(refs))
	for _, ref := range refs {
		f := v.Findings[ref]
		out = append(out, summary.Finding{ID: ref, Reporters: []summary.Reviewer{summary.ReviewerHeimdall},
			Tier: f.Tier, State: f.State, Severity: f.Severity, Location: places[ref], Claim: f.Claim,
			Result: findingResult(f, head)})
	}
	return out
}

// placesOf maps a finding ref to the place its finding.filed event named.
func placesOf(events []change.Event) map[string]finding.Location {
	out := map[string]finding.Location{}
	for _, e := range events {
		if e.Type != change.EventFindingFiled {
			continue
		}
		p, err := change.Decode(e)
		if err != nil {
			continue
		}
		if rec, ok := p.(change.FindingFiled); ok && rec.Location != nil {
			out[rec.Ref.String()] = *rec.Location
		}
	}
	return out
}

// findingResult is what the last proof run said about a finding, or nothing when it said nothing about this head. A
// result for another head is stale and is not shown as current: a new head invalidates the proof of the old one.
func findingResult(f change.FindingInfo, head id.SHA) proof.Finding {
	if f.Proof == nil || (head != "" && f.Proof.Head != head) {
		return proof.Finding{}
	}
	return proof.Finding{Class: f.Proof.Class, Dropped: f.Proof.Dropped, Reason: f.Proof.Reason,
		Blocking: f.Proof.Blocking}
}

// proofOutcome is the proof result the log holds for this head, base and policy, or unknown: no result when it holds
// none or holds one for other inputs. The gate never makes a pass out of a missing result (ACTION_MODE 5.4).
func proofOutcome(v change.View, head, base id.SHA, hash string) proof.Outcome {
	if v.Action == nil || v.Action.Proof == nil {
		return proof.Outcome{Kind: proof.KindUnknown, Reason: proof.ReasonNoResult}
	}
	p := v.Action.Proof
	if !p.Done || p.Head != head || p.Base != base || p.PolicyHash != hash {
		return proof.Outcome{Kind: proof.KindUnknown, Reason: proof.ReasonNoResult}
	}
	return proof.Outcome{Kind: p.Kind, Reason: p.Reason, Counts: p.Counts.Counts(), Cost: p.Cost.Spend()}
}
