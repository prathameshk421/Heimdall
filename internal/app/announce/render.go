package announce

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
)

// Render is the status message of a change (AGENT_INTERACTION_ARCH 4.1, H7): six lines from the view and its needs,
// nothing a person or a model wrote. The layout is the spec in testdata/plan/status/README.md.
func Render(v change.View, missing []change.Need) string {
	lines := []string{string(v.Trigger.Ticket) + " · " + summary(v, missing)}
	row := func(label, text string) { lines = append(lines, fmt.Sprintf("%-10s%s", label, text)) }
	row("Tests", testsText(v))
	row("PR", prText(v))
	row("Reviews", reviewsText(v))
	row("Findings", findingsText(v))
	row("Next", nextText(missing))
	return strings.Join(lines, "\n") + "\n"
}

func summary(v change.View, missing []change.Need) string {
	switch {
	case v.Closed != nil && v.Closed.Merged:
		return "merged"
	case v.Closed != nil:
		return "closed without merge"
	case len(missing) == 1:
		return "1 need"
	}
	return strconv.Itoa(len(missing)) + " needs"
}

func testsText(v change.View) string {
	t := v.Tests
	if t == nil {
		return "none yet"
	}
	names := map[string]bool{}
	var acs []string
	for ac, tests := range t.ACMap {
		acs = append(acs, ac)
		for _, n := range tests {
			names[n] = true
		}
	}
	sort.Slice(acs, func(i, j int) bool { return number(acs[i], "AC") < number(acs[j], "AC") })
	text := plural(len(names), "test") + " for " + strings.Join(acs, ", ") + " · submitted on " + short(string(t.SHA))
	if t.Verified != nil {
		text += " · verified"
	}
	if t.Committed != nil {
		text += " · committed " + short(string(t.Committed.SHA))
	}
	return text
}

func prText(v change.View) string {
	pr := v.PR
	if pr == nil {
		return "none yet"
	}
	text := "#" + strconv.Itoa(pr.Number) + " by " + string(pr.Author) + " · head " + short(string(pr.HeadSHA))
	switch s := pr.Slop; {
	case s == nil || s.SHA != pr.HeadSHA:
		text += " · slop not run"
	case s.Pass:
		text += " · slop passed"
	default:
		text += " · slop failed: " + strings.Join(s.FailingRules, ", ")
	}
	if acc := pr.CI[pr.HeadSHA]["heimdall/acceptance"]; acc != "" {
		text += " · acceptance " + string(acc)
	} else {
		text += " · acceptance not reported"
	}
	if v.Closed != nil {
		text += map[bool]string{true: " · merged", false: " · closed"}[v.Closed.Merged]
	}
	return text
}

func reviewsText(v change.View) string {
	if v.PR == nil || v.PR.Picked == nil {
		return "not picked"
	}
	var parts []string
	for _, who := range v.PR.Picked.People {
		r, ok := v.Reviews[string(who)]
		switch {
		case !ok:
			parts = append(parts, string(who)+" waiting")
		case r.Clean:
			parts = append(parts, string(who)+" clean on "+short(string(r.HeadSHA)))
		default:
			parts = append(parts, string(who)+" "+plural(len(r.FindingRefs), "finding")+" on "+short(string(r.HeadSHA)))
		}
	}
	return strings.Join(parts, " · ")
}

func findingsText(v change.View) string {
	if len(v.Findings) == 0 {
		return "none"
	}
	refs := make([]string, 0, len(v.Findings))
	for ref := range v.Findings {
		refs = append(refs, ref)
	}
	sort.Slice(refs, func(i, j int) bool { return number(refs[i], "F") < number(refs[j], "F") })
	parts := make([]string, 0, len(refs))
	for _, ref := range refs {
		f := v.Findings[ref]
		parts = append(parts, ref+" "+string(f.Tier)+" "+string(f.Severity)+" "+string(f.State))
	}
	return strings.Join(parts, " · ")
}

func nextText(missing []change.Need) string {
	if len(missing) == 0 {
		return "nothing"
	}
	texts := make([]string, 0, len(missing))
	for _, n := range missing {
		texts = append(texts, n.Text)
	}
	return strings.Join(texts, "; ")
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return strconv.Itoa(n) + " " + word + "s"
}

func number(s, prefix string) int {
	n, _ := strconv.Atoi(strings.TrimPrefix(s, prefix))
	return n
}
