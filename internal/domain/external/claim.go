package external

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
)

var (
	reDetailsTag = regexp.MustCompile(`(?i)</?details\b[^>]*>`)
	reSub        = regexp.MustCompile(`(?is)<sub>.*?</sub>`)
	reImage      = regexp.MustCompile(`!\[[^\]\n]*\]\([^)\n]*\)`)
	reEmptyLink  = regexp.MustCompile(`\[\s*\]\([^)\n]*\)`)
	reHTMLTag    = regexp.MustCompile(`(?i)</?(?:br|p|sub|sup|b|i|strong|em|img|hr|a|div|span|summary|blockquote|picture|source|ul|ol|li|h[1-6])\b[^>\n]{0,200}>`)
	// reRule is a horizontal rule line, which Devin writes before its feedback footer.
	reRule = regexp.MustCompile(`(?m)^[ \t]*(?:-{3,}|\*{3,}|_{3,})[ \t]*$`)

	// reMacroHeader is the severity Macroscope puts in front of a finding: an emoji and a word, sometimes bold
	// ("🟡 **Medium**"). The emoji alone is not read: the word decides.
	reMacroHeader = regexp.MustCompile(`^\s*([🔴🟠🟡🟢])\s*\**\s*(Critical|High|Medium|Low)\s*\**\s*`)
	// rePathLine is the "path:line" Macroscope repeats from the API. It is at the start of the claim only, and it needs
	// a path character before the colon, so "1:1 ratio" stays a claim.
	rePathLine = regexp.MustCompile("^\\s*`?[^\\s:`]*[./][^\\s:`]*:\\d+(?:-\\d+)?`?\\s*")
	// reLeadEmoji is the severity emoji that opens a Devin finding, after the severity has been read from the raw body.
	reLeadEmoji = regexp.MustCompile(`^\s*[🔴🟠🟡🟢]\s*`)
	// reBlobURL is a GitHub blob link on its own line, which Macroscope uses instead of "path:line".
	reBlobURL = regexp.MustCompile(`(?m)^[ \t]*https://github\.com/[^\s)]+/blob/[0-9a-fA-F]+/[^\s)#]*(?:#L[0-9]+)?[ \t]*$`)
	// reMacroFooter is the line Macroscope appends when the finding is fixed or gone. It is a resolution signal, not
	// part of the claim.
	reMacroFooter = regexp.MustCompile(`(?m)^[ \t]*(?:✅[ \t]*)?(?:No longer relevant as of|Resolved in|Addressed in|Code refactored as of)[ \t]+[0-9a-f]{7,40}[ \t]*$`)
	// reToolFooter is the line a tool appends to ask for feedback or say where it was posted. Macroscope sometimes
	// puts its fix prompt in a blockquote.
	reToolFooter = regexp.MustCompile(`(?m)^[ \t]*(?:>[ \t]*)?(?:🚀[ \t]*\**Want me to fix this\?.*|Posted via Macroscope.*|Useful\? React with.*|\*?Was this helpful\?.*)$`)
	// reRefusal is a tool saying it could not read the request, which is a fact about its access and not a finding.
	reRefusal = regexp.MustCompile(`(?i)^(?:sorry,? )?i(?:'m| am)? (?:unable|cannot|can't|can not) (?:to )?(?:act|help|review|comment)`)

	// reCodexBadge is the priority badge of the OpenAI Codex connector: "![P1 Badge](https://img.shields.io/...)".
	reCodexBadge = regexp.MustCompile(`!\[P([0-3]) Badge\]`)
	// reDevinEmoji is the emoji that opens a Devin finding: it states the severity.
	reDevinEmoji = regexp.MustCompile(`^\s*([🔴🟠🟡🟢])`)
)

// parsed is what is read from a body.
type parsed struct {
	claim  string
	sev    finding.Severity
	stated bool
	style  bool
}

// prepBody makes the body safe to read: valid UTF-8, one newline form, and at most MaxBodyBytes.
func prepBody(body string) string {
	body = strings.ToValidUTF8(body, "")
	if len(body) > MaxBodyBytes {
		body = strings.ToValidUTF8(body[:MaxBodyBytes], "")
	}
	return strings.ReplaceAll(body, "\r\n", "\n")
}

// parseBody turns a body into a claim and the severity and category the tool states (ACTION_MODE 6.2, steps 1 and 2).
func parseBody(t Tool, body string) parsed {
	text := stripComments(body)
	text = stripDetails(text)
	text = stripSuggestions(text)
	text = reSub.ReplaceAllString(text, " ")
	text = reImage.ReplaceAllString(text, " ")
	text = reEmptyLink.ReplaceAllString(text, " ")
	text = reHTMLTag.ReplaceAllString(text, " ")
	p := parsed{sev: finding.SeverityMedium}
	switch t {
	case ToolCodeRabbit:
		text = readCodeRabbit(text, &p)
	case ToolBugbot:
		text = readBugbot(text, &p)
	case ToolGreptile:
		text = readGreptile(text, &p)
	case ToolMacroscope:
		text = readMacroscope(text, &p)
	case ToolCodex:
		text = readCodex(body, text, &p)
	case ToolDevin:
		text = readDevin(text, &p)
	case ToolCopilot:
	}
	p.claim = limit(flatten(text))
	if reRefusal.MatchString(p.claim) {
		p.claim = ""
	}
	return p
}

// readCodeRabbit reads the header that opens a CodeRabbit comment, and removes it. There are two: the
// "_category_ | _severity_" line, and from November 2026 "**<picture>...</picture> Minor** · **Category** · **Quick
// win**", which is read after the HTML is gone.
func readCodeRabbit(text string, p *parsed) string {
	m := reCodeRabbitHeader.FindStringSubmatchIndex(text)
	if m != nil {
		head := strings.ToLower(text[m[2]:m[3]] + " " + text[m[4]:m[5]])
		severityWord(head, p)
		return text[:m[0]] + " " + text[m[1]:]
	}
	m = reCodeRabbitBold.FindStringSubmatchIndex(text)
	if m == nil {
		return text
	}
	severityWord(strings.ToLower(text[m[2]:m[3]]), p)
	return text[:m[0]] + " " + text[m[1]:]
}

// severityWord reads the first severity word of head. CodeRabbit states a severity, and calls a nitpick style.
func severityWord(head string, p *parsed) {
	for _, w := range []struct {
		word string
		sev  finding.Severity
	}{
		{"critical", finding.SeverityCritical}, {"major", finding.SeverityHigh}, {"minor", finding.SeverityMedium},
		{"trivial", finding.SeverityLow}, {"nitpick", finding.SeverityLow},
	} {
		if strings.Contains(head, w.word) {
			p.sev, p.stated = w.sev, true
			break
		}
	}
	p.style = strings.Contains(head, "nitpick")
}

// readBugbot reads the "**High Severity**" line of a Bugbot comment, and removes it.
func readBugbot(text string, p *parsed) string {
	m := reBugbotSeverity.FindStringSubmatchIndex(text)
	if m == nil {
		return text
	}
	switch strings.ToLower(text[m[2]:m[3]]) {
	case "critical":
		p.sev = finding.SeverityCritical
	case "high":
		p.sev = finding.SeverityHigh
	case "low":
		p.sev = finding.SeverityLow
	default:
		p.sev = finding.SeverityMedium
	}
	p.stated = true
	return text[:m[0]] + " " + text[m[1]:]
}

// readGreptile reads the "**logic:**" prefix of a Greptile comment when the body has one. Greptile states a category,
// not a severity, and a real Greptile body is plain prose with no prefix at all.
func readGreptile(text string, p *parsed) string {
	m := reGreptilePrefix.FindStringSubmatchIndex(text)
	if m == nil {
		return text
	}
	p.style = strings.EqualFold(text[m[2]:m[3]], "style")
	return text[m[1]:]
}

// readMacroscope reads the severity line and removes the tool noise a Macroscope body carries: the emoji and the
// severity word, the "path:line" it repeats from the API, a GitHub blob link, and the footer that says the finding was
// addressed or is no longer relevant. The severity is kept.
func readMacroscope(text string, p *parsed) string {
	text = readMacroSeverity(text, p)
	text = rePathLine.ReplaceAllString(text, " ")
	text = reBlobURL.ReplaceAllString(text, " ")
	text = reMacroFooter.ReplaceAllString(text, " ")
	text = reToolFooter.ReplaceAllString(text, " ")
	return text
}

// readMacroSeverity reads the "🟠 High" line at the start of a Macroscope body.
func readMacroSeverity(text string, p *parsed) string {
	m := reMacroHeader.FindStringSubmatchIndex(text)
	if m == nil {
		return text
	}
	switch strings.ToLower(text[m[4]:m[5]]) {
	case "critical":
		p.sev = finding.SeverityCritical
	case "high":
		p.sev = finding.SeverityHigh
	case "low":
		p.sev = finding.SeverityLow
	default:
		p.sev = finding.SeverityMedium
	}
	p.stated = true
	return text[m[1]:]
}

// readCodex reads the priority badge of the OpenAI Codex connector, from the raw body: the badge sits inside the
// <sub> footer that the clean-up removes. P0 is critical, P1 high, P2 medium and P3 low, as the connector's own table
// and the badges it posted say.
func readCodex(raw, text string, p *parsed) string {
	if m := reCodexBadge.FindStringSubmatch(raw); m != nil {
		p.stated = true
		switch m[1] {
		case "0":
			p.sev = finding.SeverityCritical
		case "1":
			p.sev = finding.SeverityHigh
		case "2":
			p.sev = finding.SeverityMedium
		default:
			p.sev = finding.SeverityLow
		}
	}
	text = reToolFooter.ReplaceAllString(text, " ")
	return text
}

// readDevin reads the emoji that opens a Devin finding and removes it with the feedback footer. The emoji is read from
// the text after the HTML comments are gone, because the JSON payload comment comes first in the body.
func readDevin(text string, p *parsed) string {
	if m := reDevinEmoji.FindStringSubmatch(text); m != nil {
		p.stated = true
		switch m[1] {
		case "🔴":
			p.sev = finding.SeverityCritical
		case "🟠":
			p.sev = finding.SeverityHigh
		case "🟢":
			p.sev = finding.SeverityLow
		default:
			p.sev = finding.SeverityMedium
		}
	}
	text = reLeadEmoji.ReplaceAllString(text, " ")
	text = reToolFooter.ReplaceAllString(text, " ")
	text = reRule.ReplaceAllString(text, " ")
	return text
}

// stripComments removes every HTML comment. A comment that never closes takes the rest of the text with it.
func stripComments(s string) string {
	var b strings.Builder
	for {
		i := strings.Index(s, "<!--")
		if i < 0 {
			b.WriteString(s)
			return b.String()
		}
		b.WriteString(s[:i])
		b.WriteByte(' ')
		j := strings.Index(s[i+4:], "-->")
		if j < 0 {
			return b.String()
		}
		s = s[i+4+j+3:]
	}
}

// stripDetails removes every <details> block, nested ones included. A block that never closes takes the rest.
func stripDetails(s string) string {
	var b strings.Builder
	depth, pos := 0, 0
	for _, m := range reDetailsTag.FindAllStringIndex(s, -1) {
		opening := s[m[0]+1] != '/'
		switch {
		case opening:
			if depth == 0 {
				b.WriteString(s[pos:m[0]])
				b.WriteByte(' ')
			}
			depth++
		case depth > 0:
			depth--
			if depth == 0 {
				pos = m[1]
			}
		default:
			b.WriteString(s[pos:m[0]])
			pos = m[1]
		}
	}
	if depth == 0 {
		b.WriteString(s[pos:])
	}
	return b.String()
}

// stripSuggestions removes the fenced suggestion blocks. A block that never closes takes the rest.
func stripSuggestions(s string) string {
	var out []string
	inside := false
	for _, line := range strings.Split(s, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case inside:
			inside = trimmed != "```"
		case strings.HasPrefix(trimmed, "```suggestion"):
			inside = true
		default:
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

// flatten joins the lines of the text into one line. A line that is a heading or a whole bold title gets a full stop,
// so the title and the text after it stay two sentences. Bold marks are removed line by line, so a title that was bold
// does not leave a space at the front of the claim.
func flatten(s string) string {
	var parts []string
	for _, line := range strings.Split(s, "\n") {
		raw := strings.TrimSpace(line)
		if raw == "" {
			continue
		}
		line = strings.TrimSpace(strings.ReplaceAll(raw, "**", ""))
		if line == "" {
			continue
		}
		if strings.HasPrefix(raw, "#") || (strings.HasPrefix(raw, "**") && strings.HasSuffix(raw, "**")) {
			line = strings.TrimSpace(strings.TrimLeft(line, "#"))
			if !strings.ContainsAny(line[len(line)-1:], ".!?:;") {
				line += "."
			}
		}
		parts = append(parts, line)
	}
	return strings.Join(strings.Fields(strings.Join(parts, " ")), " ")
}

// limit cuts a claim to the FindingIn limit, in runes, and marks the cut.
func limit(s string) string {
	if utf8.RuneCountInString(s) <= finding.ClaimMaxChars {
		return s
	}
	r := []rune(s)[:finding.ClaimMaxChars-1]
	return strings.TrimRight(string(r), " ") + "…"
}
