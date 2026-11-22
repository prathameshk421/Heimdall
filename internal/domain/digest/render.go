package digest

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/proof"
)

// limits caps each list of the page. A report of a week of pull requests is far below these; the caps keep a hostile
// snapshot from making a page nobody can read.
const (
	maxCauseLines = 12
	maxMissLines  = 12
	maxName       = 120
	maxReasonText = 200
)

// zwsp is a zero width space. It breaks an autolink or a mention without changing how the text looks.
const zwsp = "\u200b"

// RenderMarkdown returns the report as one GitHub-flavoured Markdown page. It never fails and the page is never longer
// than MaxBytes. The same Report always gives the same page: the lists are already sorted by Compute, and nothing here
// reads a clock, a map order or the environment.
func RenderMarkdown(r Report) string {
	var b strings.Builder
	writeHeader(&b, r)
	writeShadow(&b, r)
	writeMisses(&b, r)
	writeFooter(&b, r)
	out := b.String()
	if len(out) > MaxBytes {
		return cutBytes(out, MaxBytes)
	}
	return out
}

// writeHeader writes the title and the period, and says how many records were left out.
func writeHeader(b *strings.Builder, r Report) {
	fmt.Fprintf(b, "# %s: %s\n\n", shadowTitle(r), repoText(r.Repo))
	fmt.Fprintf(b, "Period: %s\n\n", periodText(r.From, r.To))
	if r.Outside > 0 || r.Untimed > 0 || r.Malformed > 0 {
		fmt.Fprintf(b, "Left out of this report: %s.\n\n", leftOutText(r))
	}
}

// writeShadow writes the shadow digest of A4.
func writeShadow(b *strings.Builder, r Report) {
	s := r.Shadow
	fmt.Fprintf(b, "## %s\n\n", shadowTitle(r))
	if s.Checked+s.Unchecked == 0 {
		fmt.Fprintf(b, "No pull request was checked in this period, so there is nothing to say about readiness.\n\n")
	} else {
		fmt.Fprintf(b, "%s in this period. Checked: %d. Not checked: %d. Shadow: %d. Enforce: %d.\n\n",
			plural(s.Checked+s.Unchecked, "pull request", "pull requests"), s.Checked, s.Unchecked, s.Shadow, s.Enforce)
	}
	if s.Checked > 0 {
		writeStanding(b, s)
	}
	if len(s.Causes) > 0 {
		fmt.Fprintf(b, "Top would-block causes:\n\n")
		for i, c := range s.Causes {
			if i >= maxCauseLines {
				fmt.Fprintf(b, "- and %s more, not listed to keep this report short\n\n", plural(len(s.Causes)-i, "cause", "causes"))
				break
			}
			fmt.Fprintf(b, "- %s\n", causeText(c))
		}
		b.WriteString("\n")
	} else if s.WouldBlock > 0 {
		fmt.Fprintf(b, "The would-block causes were not in this snapshot.\n\n")
	}
	fmt.Fprintf(b, "Proof runs: %d (%d blocked, %d unknown, %d not reproduced, %d skipped trivial).\n\n",
		s.Runs, s.RunBlocked, s.RunUnknown, s.RunNotReproduced, s.RunTrivial)
	fmt.Fprintf(b, "Candidate findings: %d proven (%d blocking, %d need a person), %d dropped, %d not checked.\n",
		s.Proven, s.Blocking, s.NeedsPerson, s.Dropped, s.NotChecked)
	if s.BreakGlassOpen > 0 || s.BreakGlassReviewed > 0 {
		fmt.Fprintf(b, "Break-glass: %d open, %d reviewed.\n", s.BreakGlassOpen, s.BreakGlassReviewed)
	}
	b.WriteString("\n")
	writeCost(b, s.Cost)
}

// writeStanding writes how the period would have stood under enforce mode. It is written only when a pull request was
// checked: a period where nobody checked anything says nothing about readiness.
func writeStanding(b *strings.Builder, s ShadowDigest) {
	fmt.Fprintf(b, "- would pass: %d of %d (%s)\n", s.WouldPass, s.Checked, percent(s.PassBasisPoints))
	if s.WouldBlockUnknownOnly > 0 {
		fmt.Fprintf(b, "- would block: %d of %d, of which %d would pass once the missing facts were known\n",
			s.WouldBlock, s.Checked, s.WouldBlockUnknownOnly)
	} else {
		fmt.Fprintf(b, "- would block: %d of %d\n", s.WouldBlock, s.Checked)
	}
	fmt.Fprintf(b, "- not checked: %d of %d had no result from either check\n", s.Unchecked, s.Checked+s.Unchecked)
	fmt.Fprintf(b, "- readiness: the check is %s\n\n", readiness(s))
}

// shadowTitle is the heading of the A4 section. A week in enforce mode is not a shadow digest, so it says so.
func shadowTitle(r Report) string {
	if r.Shadow.Enforce > 0 && r.Shadow.Shadow == 0 {
		return "Digest"
	}
	return "Shadow digest"
}

// writeCost writes the spend of the period.
func writeCost(b *strings.Builder, c CostStats) {
	if c.Measured == 0 {
		fmt.Fprintf(b, "Cost: no run in this period recorded a spend.\n\n")
		return
	}
	fmt.Fprintf(b, "Cost: %s in total over %s, mean %s, p50 %s, p95 %s.\n\n", money(c.TotalMicros),
		plural(c.Measured, "pull request", "pull requests"), money(c.MeanMicros), money(c.P50Micros), money(c.P95Micros))
}

// writeMisses writes what Heimdall did not check, what it did not know, and how long each check took, which is A12.
func writeMisses(b *strings.Builder, r Report) {
	m := r.Misses
	fmt.Fprintf(b, "## What we did not check\n\n")
	writeMissList(b, "Clauses not checked, by clause", textLines(m.NotCheckedClauses, "clause"))
	writeMissList(b, "Clauses not checked, by reason", textLines(m.NotCheckedReasons, "reason"))
	writeMissList(b, "Candidate findings not checked, by reason",
		reasonLines(m.CandidateNotChecked, "candidate", "candidates"))
	writeMissList(b, "Proof runs that ended unknown, by reason", reasonLines(m.UnknownReasons, "run", "runs"))
	writeMissList(b, "Candidate findings dropped, by reason", reasonLines(m.DroppedReasons, "candidate", "candidates"))
	if emptyMisses(m) && r.PRs > 0 {
		fmt.Fprintf(b, "Every result in this period was checked and was known.\n\n")
	} else if r.PRs == 0 {
		fmt.Fprintf(b, "No result was recorded in this period, so there was nothing to miss.\n\n")
	}
	writeLatency(b, "Fast check wall time", m.Fast)
	writeLatency(b, "Proof run wall time", m.Proof)
}

// writeMissList writes one titled list, or a line that says the list is empty.
func writeMissList(b *strings.Builder, title string, lines []string) {
	if len(lines) == 0 {
		return
	}
	fmt.Fprintf(b, "%s:\n\n", title)
	for i, line := range lines {
		if i >= maxMissLines {
			fmt.Fprintf(b, "- and %s more, not listed to keep this report short\n", plural(len(lines)-i, "reason", "reasons"))
			break
		}
		fmt.Fprintf(b, "- %s\n", line)
	}
	b.WriteString("\n")
}

// writeLatency writes the percentiles of one check.
func writeLatency(b *strings.Builder, title string, l Latency) {
	if l.Measured == 0 {
		fmt.Fprintf(b, "%s: not measured in this period.\n\n", title)
		return
	}
	fmt.Fprintf(b, "%s: p50 %s, p95 %s, max %s, mean %s, over %s.\n\n", title, ms(l.P50MS), ms(l.P95MS), ms(l.MaxMS),
		ms(l.MeanMS), plural(l.Measured, "run", "runs"))
}

// writeFooter says what the numbers are not, in the mode the period was checked under.
func writeFooter(b *strings.Builder, r Report) {
	fmt.Fprintf(b, "A digest is a summary of recorded results. It never decides a merge, and a pull request without a "+
		"result is not a pass. %s\n", footerMode(r))
}

// footerMode says what the mode of the period means for these numbers. A period with any shadow pull request is read
// as shadow, because a shadow result blocks nothing.
func footerMode(r Report) string {
	if r.Shadow.Shadow > 0 || r.Shadow.Enforce == 0 {
		return "These pull requests ran in shadow mode, so no pull request was blocked by these numbers."
	}
	return "These pull requests ran in enforce mode: the fast check decides a merge where the team requires it."
}

// textLines turns text counts into list lines. The text is escaped, because a clause id or a reason can come from a
// snapshot another tool wrote.
func textLines(counts []TextCount, what string) []string {
	out := make([]string, 0, len(counts))
	for _, c := range counts {
		label := code(c.Text)
		if c.Text == "" {
			label = "no " + what + " given"
		}
		out = append(out, fmt.Sprintf("%s: %s", label, plural(c.Count, "pull request", "pull requests")))
	}
	return out
}

// reasonLines turns reason counts into list lines, with the words package proof gives them and the noun the list counts.
func reasonLines(counts []ReasonCount, one, many string) []string {
	out := make([]string, 0, len(counts))
	for _, c := range counts {
		out = append(out, fmt.Sprintf("%s: %s", reasonText(c), plural(c.Count, one, many)))
	}
	return out
}

// reasonText is a reason as a person reads it. A known reason uses the words of package proof; a value this package
// does not define is shown as text, so it cannot pass for a real reason.
func reasonText(c ReasonCount) string {
	switch {
	case strings.TrimSpace(c.Reason) == "":
		return "no reason given"
	case c.Known:
		return escape(clean(reasonWords(c.Reason), maxReasonText))
	}
	return code(c.Reason)
}

// reasonWords is the words of package proof for a reason or a drop reason. Both lists are read, because one report
// names both, and a reason this package does not define is shown as its own text.
func reasonWords(r string) string {
	if words, ok := reasonWordsTable()[r]; ok {
		return words
	}
	return r
}

// reasonWordsTable maps every reason and drop reason of package proof to the words a person reads. It is a function,
// so no package-level value can be changed by a caller.
func reasonWordsTable() map[string]string {
	words := map[string]string{
		string(proof.ReasonBudget):          "budget hit",
		string(proof.ReasonMonthlyCap):      "monthly cap reached",
		string(proof.ReasonDeadline):        "timeout",
		string(proof.ReasonSuperseded):      "superseded",
		string(proof.ReasonForkNoKey):       "fork without a key",
		string(proof.ReasonCannotIsolate):   "runner cannot isolate pull request code",
		string(proof.ReasonInputMissing):    "a declared input was missing",
		string(proof.ReasonWorkflowChanged): "the pull request changed the workflow",
		string(proof.ReasonStateUnverified): "the state failed verification",
		string(proof.ReasonSandbox):         "sandbox setup or dependency fetch failed",
		string(proof.ReasonNoIntent):        "no intent to test",
		string(proof.ReasonPlanRefused):     "the test plan was refused",
		string(proof.ReasonModelFailure):    "the model call failed",
		string(proof.ReasonNoResult):        "no result",
		string(proof.ReasonTurnCap):         "turn cap reached",
		string(proof.ReasonNoProposal):      "no proposal before the stall limit",
		string(proof.ReasonTrivial):         "trivial pull request",
		string(proof.ReasonNotProvable):     "not provable by a test or tool",
		string(proof.DropRefuted):           "refuted",
		string(proof.DropNoProof):           "no test reproduced it",
		string(proof.DropOutdated):          "outdated",
		string(proof.DropNoLocation):        "no location",
	}
	return words
}

// causeText is one cause as a person reads it. A cause that names no id is only its kind; a cause that names one puts
// the id in a code span, because a clause id or a reason comes from a snapshot.
func causeText(c Cause) string {
	label := causeLabel(c)
	if clean(c.ID, maxName) == "" {
		return label
	}
	return label + ": " + code(c.ID)
}

// causeLabel says what kind of cause one entry is.
func causeLabel(c Cause) string {
	switch c.Kind {
	case CauseFastUnmet:
		return "fast check unmet"
	case CauseFastUnknown:
		return "fast check unknown"
	case CauseProofBlocking:
		return "proof blocked"
	case CauseProofUnknown:
		return "proof unknown"
	}
	return "cause"
}

// readiness says, in one phrase, how close the team is to enforcing the check.
func readiness(s ShadowDigest) string {
	if s.Checked == 0 {
		return "not measured yet"
	}
	switch {
	case s.WouldBlock == 0:
		return "ready to enforce: no pull request would have blocked in this period"
	case s.WouldBlockUnknownOnly == s.WouldBlock:
		return "not ready to enforce: every would-block was a missing fact, so fix the inputs first"
	}
	return "not ready to enforce: some pull requests would have blocked on a real finding"
}

// emptyMisses reports whether the misses report has nothing at all to say.
func emptyMisses(m Misses) bool {
	return len(m.NotCheckedClauses) == 0 && len(m.NotCheckedReasons) == 0 && len(m.CandidateNotChecked) == 0 &&
		len(m.UnknownReasons) == 0 && len(m.DroppedReasons) == 0
}

// leftOutText says how many records the report left out, and why.
func leftOutText(r Report) string {
	var parts []string
	if r.Outside > 0 {
		parts = append(parts, fmt.Sprintf("%d outside the period", r.Outside))
	}
	if r.Untimed > 0 {
		parts = append(parts, fmt.Sprintf("%d with no time", r.Untimed))
	}
	if r.Malformed > 0 {
		parts = append(parts, fmt.Sprintf("%d that could not be true", r.Malformed))
	}
	return strings.Join(parts, ", ")
}

// repoText shows the repository, or says it was not given. The name comes from a snapshot, so it is escaped.
func repoText(repo string) string {
	if strings.TrimSpace(repo) == "" {
		return "no repository given"
	}
	return code(repo)
}

// periodText writes the period in dates. A missing end is open.
func periodText(from, to time.Time) string {
	switch {
	case from.IsZero() && to.IsZero():
		return "all recorded results"
	case from.IsZero():
		return "up to " + date(to)
	case to.IsZero():
		return "from " + date(from)
	}
	return date(from) + " to " + date(to)
}

// date writes one day in UTC, so the same report reads the same in every time zone.
func date(t time.Time) string { return t.UTC().Format("2006-01-02") }

// percent writes basis points as a percentage with one decimal: 8750 is "87.5%".
func percent(bp int) string {
	if bp <= 0 {
		return "0.0%"
	}
	if bp >= 10_000 {
		return "100.0%"
	}
	return strconv.FormatFloat(float64(bp)/100, 'f', 1, 64) + "%"
}

// money writes micro dollars as a dollar amount. A real amount under one cent is "<$0.01", so a small cost never
// reads as free.
func money(micros int64) string {
	switch {
	case micros <= 0:
		return "$0.00"
	case micros < 5_000:
		return "<$0.01"
	}
	return "$" + strconv.FormatFloat(float64(micros)/1_000_000, 'f', 2, 64)
}

// ms writes a duration in milliseconds as a person reads it.
func ms(v int64) string {
	switch {
	case v <= 0:
		return "0s"
	case v < 1_000:
		return strconv.FormatInt(v, 10) + "ms"
	case v < 60_000:
		return strconv.FormatFloat(float64(v)/1_000, 'f', 1, 64) + "s"
	}
	return strconv.FormatInt(v/60_000, 10) + "m " + strconv.FormatInt(v%60_000/1_000, 10) + "s"
}

// plural writes a count with its noun.
func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}

// clean turns an untrusted string into one plain line of at most limit runes. Every kind of white space and every
// control character becomes one space, bytes that are not UTF-8 become a space, and format characters (bidirectional
// overrides, zero width characters) are removed, so the text cannot reorder what a person reads. A cut text ends with
// "…".
func clean(s string, limit int) string {
	var b strings.Builder
	space := false
	for i := 0; i < len(s); {
		r, w := utf8.DecodeRuneInString(s[i:])
		i += w
		switch {
		case r == utf8.RuneError && w == 1, unicode.IsSpace(r), unicode.IsControl(r):
			space = true
		case unicode.Is(unicode.Cf, r):
			// Removed.
		default:
			if space && b.Len() > 0 {
				b.WriteByte(' ')
			}
			space = false
			b.WriteRune(r)
		}
	}
	return cut(b.String(), limit)
}

// cut keeps at most limit runes of s, and ends a cut text with "…".
func cut(s string, limit int) string {
	if limit < 1 || utf8.RuneCountInString(s) <= limit {
		return s
	}
	return string([]rune(s)[:limit-1]) + "…"
}

// cutBytes keeps at most n bytes of s without splitting a rune. It is the last guard on the page size.
func cutBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	end := n
	for end > 0 && !utf8.RuneStart(s[end]) {
		end--
	}
	return s[:end]
}

// escape makes one line of untrusted text safe in the middle of a line of Markdown. It cannot open HTML, an image, a
// link or a comment, cannot start emphasis, code or a table cell, cannot mention a person or make an issue reference,
// and cannot make a bare link.
func escape(s string) string {
	var b strings.Builder
	runes := []rune(s)
	for i, r := range runes {
		b.WriteString(escapeRune(runes, i, r))
	}
	return breakWWW(b.String())
}

// escapeRune is the escape of the rune at runes[i].
func escapeRune(runes []rune, i int, r rune) string {
	switch r {
	case '&':
		return "&amp;"
	case '<':
		return "&lt;"
	case '>':
		return "&gt;"
	case '@':
		return "@" + zwsp
	case '#':
		if i+1 < len(runes) && runes[i+1] >= '0' && runes[i+1] <= '9' {
			return `\#` + zwsp
		}
		return `\#`
	case ':':
		if strings.HasPrefix(string(runes[i+1:min(len(runes), i+3)]), "//") {
			return ":" + zwsp
		}
	}
	if strings.ContainsRune(backslashed, r) {
		return `\` + string(r)
	}
	return string(r)
}

// backslashed are the characters that carry a backslash: those that start emphasis, code, a link, strike through, a
// table cell or math.
const backslashed = "\\`*_[]()~|!${}"

// breakWWW puts a zero width space after each "www" that is followed by a dot, so no bare "www." link forms.
func breakWWW(s string) string {
	lower := strings.ToLower(s)
	var b strings.Builder
	last := 0
	for from := 0; ; {
		i := strings.Index(lower[from:], "www.")
		if i < 0 {
			break
		}
		at := from + i + len("www")
		b.WriteString(s[last:at])
		b.WriteString(zwsp)
		last, from = at, at
	}
	b.WriteString(s[last:])
	return b.String()
}

// code cleans and cuts one untrusted string and wraps it in a code span that nothing inside can close. The span
// delimiter is one backtick longer than the longest run inside. It returns "" for an empty string.
func code(s string) string {
	s = clean(s, maxName)
	if s == "" {
		return ""
	}
	run, longest := 0, 0
	for _, r := range s {
		if r == '`' {
			run++
			longest = max(longest, run)
			continue
		}
		run = 0
	}
	fence := strings.Repeat("`", longest+1)
	pad := ""
	if strings.HasPrefix(s, "`") || strings.HasSuffix(s, "`") {
		pad = " "
	}
	return fence + pad + s + pad + fence
}
