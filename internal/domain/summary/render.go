package summary

import (
	"strconv"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/fastcheck"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/proof"
)

// The caps on untrusted text, in runes before escaping.
const (
	maxPath   = 160
	maxClaim  = 240
	maxName   = 120
	maxReason = 200
	// maxClauses is the most clauses shown. The fast check has 8, so this only guards a caller with more.
	maxClauses = 16
)

// limits caps the length of each list. Render starts high and halves every cap until the page fits.
type limits struct {
	proven, items, needs, notChecked, sources int
}

func startLimits() limits {
	return limits{proven: 40, items: 30, needs: 12, notChecked: 12, sources: 12}
}

func (l limits) halve() limits {
	return limits{l.proven / 2, l.items / 2, l.needs / 2, l.notChecked / 2, l.sources / 2}
}

func (l limits) zero() bool { return l == limits{} }

// view is the input read once: the sorted lists and the facts every section needs.
type view struct {
	in       Input
	mode     string
	stance   stance
	fast     fastState
	clauses  []fastcheck.Clause
	proven   []Finding
	unproven []Finding
	blocking int
}

func newView(in Input) view {
	v := view{in: in, mode: modeOf(in.Action), stance: proofStance(in.Action)}
	v.fast, v.clauses = assessFast(in.Fast)
	v.proven, v.unproven = split(in.Findings)
	for _, f := range v.proven {
		if f.Result.Blocking {
			v.blocking++
		}
	}
	return v
}

// Render writes the proof summary comment as GitHub-flavoured Markdown. The first line is the hidden marker. The page is
// the same for the same input, never longer than MaxBytes, and every untrusted string in it is escaped.
func Render(in Input) string { return renderWithin(newView(in), MaxBytes) }

// renderWithin halves the list caps until the page fits in max bytes. At zero caps every list is one "and N more" line,
// which fits by construction. If even that does not, the short page is all that is written.
func renderWithin(v view, size int) string {
	for l := startLimits(); ; l = l.halve() {
		out := v.render(l)
		if len(out) <= size {
			return out
		}
		if l.zero() {
			return v.short()
		}
	}
}

// short is the page written when nothing else fits: the marker, the headline and one line that says why.
func (v view) short() string {
	return strings.Join([]string{v.marker(), "", "## " + v.title(), "", "The full summary does not fit in a comment."}, "\n") + "\n"
}

func (v view) marker() string {
	return Marker{Change: v.in.Change, Version: v.in.Version, Head: v.in.Head}.String()
}

// title is the headline with the mode and the short head: the one line a person reads first.
func (v view) title() string {
	return v.headline() + " · " + v.mode + " · `" + shortHead(v.in.Head) + "`"
}

func (v view) render(l limits) string {
	blocks := [][]string{
		{v.marker()},
		{"## " + v.title()},
	}
	if v.mode == policy.ActionModeShadow {
		blocks = append(blocks, []string{"Shadow mode: this reports only. It does not block a merge."})
	}
	blocks = append(blocks,
		append([]string{"### Needs a person"}, v.needsLines(l.needs)...),
		append([]string{"### Proven findings"}, v.provenLines(l.proven)...),
		v.unprovenBlock(l),
		append([]string{"### Fast check"}, v.fastLines()...),
		append([]string{"### Not checked"}, v.notCheckedLines(l.notChecked)...),
		[]string{v.costLine()},
	)
	parts := make([]string, 0, len(blocks))
	for _, b := range blocks {
		parts = append(parts, strings.Join(b, "\n"))
	}
	return strings.Join(parts, "\n\n") + "\n"
}

// more is the line that ends a list cut short. It says how many items are not listed.
func more(hidden int, noun string) string {
	return "- and " + strconv.Itoa(hidden) + " more " + noun + ", not listed to keep this comment short"
}

// limited keeps the first n lines of a list and ends it with a "more" line when it cuts.
func limited(lines []string, n int, noun string) []string {
	if len(lines) <= n {
		return lines
	}
	return append(lines[:n:n], more(len(lines)-n, noun))
}

// limitedFunc is limited for a list that is costly to write: it writes only the lines it keeps.
func limitedFunc(total, n int, line func(i int) string, noun string) []string {
	lines := make([]string, 0, min(total, n)+1)
	for i := range min(total, n) {
		lines = append(lines, line(i))
	}
	if total > n {
		lines = append(lines, more(total-n, noun))
	}
	return lines
}

func (v view) needsLines(n int) []string {
	var lines []string
	switch v.in.Proof.Kind {
	case proof.KindUnknown:
		lines = append(lines, "- The proof run is unknown ("+v.in.Proof.Reason.Text()+
			"). Only a person clears it: rerun the proof or decide by hand.")
	case proof.KindNotReproduced:
		lines = append(lines, "- No test showed that the change does what it says. A person must check the change by hand.")
	case proof.KindSkippedTrivial, proof.KindProven, proof.KindBlocked:
	}
	if k := len(v.proven) - v.blocking; k > 0 {
		lines = append(lines, "- "+plural(k, "proven finding does", "proven findings do")+
			" not block under the policy. A person decides what to do.")
	}
	for _, c := range clausesWith(v.clauses, fastcheck.StatusUnknown) {
		lines = append(lines, "- "+clauseCode(c.ID)+" is unknown: "+text(c.Reason, maxReason))
	}
	for _, c := range clausesWith(v.clauses, fastcheck.StatusUnmet) {
		if needsApprover(c.ID) {
			lines = append(lines, "- "+clauseCode(c.ID)+" needs an approver: "+text(c.Reason, maxReason))
		}
	}
	if len(lines) == 0 {
		return []string{"Nothing needs a person."}
	}
	return limited(lines, n, "items")
}

// needsApprover reports whether an unmet clause can only be settled by a person with the right to approve.
func needsApprover(id fastcheck.ClauseID) bool {
	return id == fastcheck.ClauseApproval || id == fastcheck.ClausePinnedTests || id == fastcheck.ClauseHeimdallFiles
}

func (v view) provenLines(n int) []string {
	if len(v.proven) == 0 {
		return []string{"Nothing is proven."}
	}
	return limitedFunc(len(v.proven), n, func(i int) string { return v.provenItem(v.proven[i]) }, "proven findings")
}

// where is the place of a finding as a code span: "path:12", "path:12-15" or just "path". An empty path is no place.
func where(f Finding) string {
	path := f.Location.File
	if clean(path, maxPath) == "" {
		return "no location"
	}
	a, b := f.Location.Lines[0], f.Location.Lines[1]
	switch {
	case a < 1 || b < a:
		return code(path, maxPath)
	case a == b:
		return code(path+":"+strconv.Itoa(a), maxPath)
	}
	return code(path+":"+strconv.Itoa(a)+"-"+strconv.Itoa(b), maxPath)
}

func (v view) provenItem(f Finding) string {
	var facts []string
	if s := text(string(f.Severity), 16); s != "" {
		facts = append(facts, "severity "+s)
	}
	facts = append(facts, "tier "+text(string(f.Tier), 8), v.blockWording(f))
	if f.State != "" && f.State != "open" {
		facts = append(facts, "state "+text(string(f.State), 32))
	}
	claim := text(f.Claim, maxClaim)
	if claim == "" {
		claim = "(no claim text)"
	}
	return "- **" + idLabel(f.ID) + "** " + where(f) + " " + claim + "\n  " +
		capital(strings.Join(facts, ", ")) + ". Evidence: " + evidenceText(f.Evidence) +
		". Raised by " + raisedBy(f) + "."
}

// clauseCode is a clause id as a code span.
func clauseCode(id fastcheck.ClauseID) string {
	if c := code(string(id), maxName); c != "" {
		return c
	}
	return "`?`"
}

func capital(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func (v view) blockWording(f Finding) string {
	switch {
	case !f.Result.Blocking:
		return "does not block"
	case v.stance == stanceWould:
		return "would block the merge"
	case v.stance == stanceAdvisory:
		return "blocking, advisory only"
	}
	return "blocks the merge"
}

func evidenceText(e Evidence) string {
	switch e.Kind {
	case EvidenceTest:
		if n := code(e.Name, maxName); n != "" {
			return "failing test " + n
		}
		return "failing test (name not recorded)"
	case EvidenceTool:
		tool, rule := code(e.Name, maxName), code(e.Rule, maxName)
		switch {
		case tool != "" && rule != "":
			return "tool " + tool + ", rule " + rule
		case tool != "":
			return "tool " + tool
		case rule != "":
			return "tool rule " + rule
		}
		return "tool result (not recorded)"
	case EvidenceRuleCard:
		if n := code(e.Name, maxName); n != "" {
			return "team rule card " + n
		}
		return "team rule card (not recorded)"
	}
	return "not recorded"
}

func raisedBy(f Finding) string {
	var names []string
	for _, r := range reporters(f) {
		names = append(names, reviewerName(r))
	}
	return strings.Join(names, " and ")
}
