package agenttools

import (
	"strings"
	"unicode"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

// duplicateClaimJaccard is the token-overlap threshold for merging two findings on overlapping lines (TECH_SPEC 11.2b, L1/L2).
const duplicateClaimJaccard = 0.6

type knownFinding struct {
	ref         id.FindingRef
	fingerprint string
	claim       string
	location    *finding.Location
	state       finding.State
}

// liveFindingsFrom extracts the currently live (block-capable) findings from existing events so a second seat or a
// later head reuses the existing ref instead of filing a duplicate (L1, L2, P2).
func liveFindingsFrom(existing []change.Event) []knownFinding {
	byRef := map[id.FindingRef]knownFinding{}
	var order []id.FindingRef
	for _, ev := range existing {
		applyKnownEvent(ev, byRef, &order)
	}
	var out []knownFinding
	for _, ref := range order {
		if k := byRef[ref]; k.state.CanBlock() {
			out = append(out, k)
		}
	}
	return out
}

func applyKnownEvent(ev change.Event, byRef map[id.FindingRef]knownFinding, order *[]id.FindingRef) {
	if ev.Type != change.EventFindingFiled && ev.Type != change.EventFindingStateChanged && ev.Type != change.EventFindingVerified {
		return
	}
	p, err := change.Decode(ev)
	if err != nil {
		return
	}
	switch v := p.(type) {
	case change.FindingFiled:
		f := change.FindingRecord(v)
		if _, seen := byRef[f.Ref]; !seen {
			*order = append(*order, f.Ref)
		}
		byRef[f.Ref] = knownFinding{ref: f.Ref, fingerprint: f.Fingerprint, claim: f.Claim, location: f.Location, state: f.State}
	case change.FindingStateChanged:
		updateKnownState(byRef, v.Ref, v.State)
	case change.FindingVerified:
		updateKnownState(byRef, v.Ref, v.State)
	}
}

func updateKnownState(byRef map[id.FindingRef]knownFinding, ref id.FindingRef, st finding.State) {
	if cur, ok := byRef[ref]; ok {
		cur.state = st
		byRef[ref] = cur
	}
}

// matchFinding reports whether input duplicates a known live finding by fingerprint or by overlapping lines and claim similarity.
func matchFinding(known []knownFinding, input finding.Input, fp string) (knownFinding, bool) {
	for _, k := range known {
		if fp != "" && k.fingerprint == fp {
			return k, true
		}
		if k.location != nil && locationsOverlap(*k.location, input.Location) && claimJaccard(k.claim, input.Claim) >= duplicateClaimJaccard {
			return k, true
		}
	}
	return knownFinding{}, false
}

func locationsOverlap(a, b finding.Location) bool {
	fa := strings.ToLower(strings.TrimSpace(a.File))
	fb := strings.ToLower(strings.TrimSpace(b.File))
	return fa == fb && a.Lines[0] <= b.Lines[1] && b.Lines[0] <= a.Lines[1]
}

func claimJaccard(a, b string) float64 {
	left, right := claimTokens(a), claimTokens(b)
	if len(left) == 0 && len(right) == 0 {
		return 1
	}
	inter := 0
	for tok := range left {
		if _, ok := right[tok]; ok {
			inter++
		}
	}
	union := len(left) + len(right) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

func claimTokens(text string) map[string]struct{} {
	set := make(map[string]struct{})
	for _, tok := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) }) {
		set[tok] = struct{}{}
	}
	return set
}
