package summary

import (
	"cmp"
	"slices"
	"strconv"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/external"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/proof"
)

// shownProven reports whether the finding is listed as proven. Only a proven finding with a tier that a test, a tool
// or a rule card gives is. A finding marked proven with tier E4 or no tier has no proof behind it, so it is counted
// as unproven: the word "proven" is never printed for it.
func shownProven(f Finding) bool {
	if f.Result.Class != proof.ClassProven {
		return false
	}
	return f.Tier == finding.TierE1 || f.Tier == finding.TierE2 || f.Tier == finding.TierE3
}

// A label is why a finding is unproven, as the summary counts it. Rank orders the labels in every list.
type label struct {
	Rank int
	Text string
}

// unprovenLabel says why a finding is not shown as proven. The words are "unproven" and "not checked", never "false":
// a test cannot show that a finding is wrong.
func unprovenLabel(f Finding) label {
	switch f.Result.Class {
	case proof.ClassDropped:
		switch f.Result.Dropped {
		case proof.DropNoProof:
			return label{0, "no test reproduced"}
		case proof.DropRefuted:
			return label{1, "refuted"}
		case proof.DropOutdated:
			return label{2, "outdated"}
		case proof.DropNoLocation:
			return label{3, "no location"}
		}
	case proof.ClassNotChecked:
		return label{notCheckedRank(f.Result.Reason), "not checked: " + f.Result.Reason.Text()}
	case proof.ClassProven:
		return label{4, "no test or tool evidence"}
	}
	return label{5, "no proof"}
}

// notCheckedRank orders the not-checked reasons as proof lists them, then the two by-design reasons.
func notCheckedRank(r proof.Reason) int {
	all := append(proof.StopReasons(), proof.ReasonTrivial, proof.ReasonNotProvable)
	if i := slices.Index(all, r); i >= 0 {
		return 10 + i
	}
	return 10 + len(all)
}

// reviewerName is how a reviewer reads. An unknown reviewer is shown as cleaned text.
func reviewerName(r Reviewer) string {
	switch r {
	case ReviewerHeimdall:
		return "Heimdall"
	case ReviewerCodeRabbit:
		return "CodeRabbit"
	case ReviewerCopilot:
		return "Copilot"
	case ReviewerBugbot:
		return "Bugbot"
	case ReviewerGreptile:
		return "Greptile"
	}
	if n := text(string(r), maxName); n != "" {
		return n
	}
	return "unknown reviewer"
}

// reviewerRank puts Heimdall first, then the tools in the order of the design, then others by name.
func reviewerRank(r Reviewer) int {
	i := slices.Index([]Reviewer{ReviewerHeimdall, ReviewerCodeRabbit, ReviewerCopilot, ReviewerBugbot, ReviewerGreptile}, r)
	if i < 0 {
		return 100
	}
	return i
}

// reporters returns the reviewers of f once each, in the order of reviewerRank and then by name. A finding with no
// reporter is raised by an unknown reviewer.
func reporters(f Finding) []Reviewer {
	rs := slices.Clone(f.Reporters)
	if len(rs) == 0 {
		rs = []Reviewer{""}
	}
	slices.SortFunc(rs, func(a, b Reviewer) int {
		return cmp.Or(cmp.Compare(reviewerRank(a), reviewerRank(b)), strings.Compare(string(a), string(b)))
	})
	return slices.Compact(rs)
}

// idLabel is the short id of a finding: "F" and 1 to 9 digits, or "F?". The id never comes from a ULID.
func idLabel(id string) string {
	if idNumber(id) < 0 {
		return "F?"
	}
	return id
}

// idNumber is the number of an id such as "F12", or -1 when it is not one.
func idNumber(id string) int {
	d, ok := strings.CutPrefix(id, "F")
	if !ok || d == "" || len(d) > 9 {
		return -1
	}
	n, err := strconv.Atoi(d)
	if err != nil || n < 0 || strconv.Itoa(n) != d {
		return -1
	}
	return n
}

func severityRank(s finding.Severity) int {
	switch s {
	case finding.SeverityCritical:
		return 0
	case finding.SeverityHigh:
		return 1
	case finding.SeverityMedium:
		return 2
	case finding.SeverityLow:
		return 3
	}
	return 4
}

func tierRank(t finding.Tier) int {
	i := slices.Index([]finding.Tier{finding.TierE1, finding.TierE2, finding.TierE3, finding.TierE4}, t)
	if i < 0 {
		return 4
	}
	return i
}

// byID orders by the number in the id, then by the id text. An id that is not valid goes last.
func byID(a, b Finding) int {
	na, nb := idNumber(a.ID), idNumber(b.ID)
	if na < 0 {
		na = 1 << 30
	}
	if nb < 0 {
		nb = 1 << 30
	}
	return cmp.Or(cmp.Compare(na, nb), strings.Compare(a.ID, b.ID))
}

// compareProven orders the proven list: blocking first, then severity, then tier, then id. The sort is stable, so the
// same input always gives the same page.
func compareProven(a, b Finding) int {
	blocking := func(f Finding) int {
		if f.Result.Blocking {
			return 0
		}
		return 1
	}
	return cmp.Or(
		cmp.Compare(blocking(a), blocking(b)),
		cmp.Compare(severityRank(a.Severity), severityRank(b.Severity)),
		cmp.Compare(tierRank(a.Tier), tierRank(b.Tier)),
		byID(a, b),
	)
}

// compareUnproven orders the unproven list by label, then by id.
func compareUnproven(a, b Finding) int {
	return cmp.Or(cmp.Compare(unprovenLabel(a).Rank, unprovenLabel(b).Rank), byID(a, b))
}

// split divides the findings into the proven list and the unproven list, each in its order.
func split(findings []Finding) (proven, unproven []Finding) {
	for _, f := range findings {
		if shownProven(f) {
			proven = append(proven, f)
		} else {
			unproven = append(unproven, f)
		}
	}
	slices.SortStableFunc(proven, compareProven)
	slices.SortStableFunc(unproven, compareUnproven)
	return proven, unproven
}

// sourceCount is what one reviewer raised that is unproven: the total and the count under each label.
type sourceCount struct {
	Reviewer Reviewer
	Total    int
	Labels   []labelCount
}

type labelCount struct {
	Label label
	N     int
}

// countBySource counts the unproven findings per reviewer and per label. A finding with several reporters counts once
// for each, because each tool's own noise is what the line is about.
func countBySource(unproven []Finding) []sourceCount {
	var out []sourceCount
	for _, f := range unproven {
		l := unprovenLabel(f)
		for _, r := range reporters(f) {
			i := slices.IndexFunc(out, func(s sourceCount) bool { return s.Reviewer == r })
			if i < 0 {
				out = append(out, sourceCount{Reviewer: r})
				i = len(out) - 1
			}
			out[i].Total++
			out[i].Labels = addLabel(out[i].Labels, l)
		}
	}
	slices.SortFunc(out, func(a, b sourceCount) int {
		return cmp.Or(cmp.Compare(reviewerRank(a.Reviewer), reviewerRank(b.Reviewer)),
			strings.Compare(string(a.Reviewer), string(b.Reviewer)))
	})
	return out
}

func addLabel(ls []labelCount, l label) []labelCount {
	i := slices.IndexFunc(ls, func(c labelCount) bool { return c.Label == l })
	if i < 0 {
		ls = append(ls, labelCount{Label: l})
		i = len(ls) - 1
	}
	ls[i].N++
	slices.SortFunc(ls, func(a, b labelCount) int {
		return cmp.Or(cmp.Compare(a.Label.Rank, b.Label.Rank), strings.Compare(a.Label.Text, b.Label.Text))
	})
	return ls
}

// ReviewerOf maps a tool of package external to its reviewer, so a caller does not convert by hand.
func ReviewerOf(t external.Tool) Reviewer { return Reviewer(t) }
