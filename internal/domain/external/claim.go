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
	case ToolCopilot:
	}
	p.claim = limit(flatten(text))
	return p
}

// readCodeRabbit reads the "_category_ | _severity_" line that opens a CodeRabbit comment, and removes it.
func readCodeRabbit(text string, p *parsed) string {
	m := reCodeRabbitHeader.FindStringSubmatchIndex(text)
	if m == nil {
		return text
	}
	head := strings.ToLower(text[m[2]:m[3]] + " " + text[m[4]:m[5]])
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
	return text[:m[0]] + " " + text[m[1]:]
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

// readGreptile reads the "**logic:**" prefix of a Greptile comment. Greptile states a category, not a severity.
func readGreptile(text string, p *parsed) string {
	m := reGreptilePrefix.FindStringSubmatchIndex(text)
	if m == nil {
		return text
	}
	p.style = strings.EqualFold(text[m[2]:m[3]], "style")
	return text[m[1]:]
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

// flatten joins the lines of the text into one line. A heading that has no end mark gets a full stop, so the title and
// the text after it stay two sentences.
func flatten(s string) string {
	var parts []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			line = strings.TrimSpace(strings.TrimLeft(line, "#"))
			line = strings.ReplaceAll(line, "**", "")
			if line != "" && !strings.ContainsAny(line[len(line)-1:], ".!?:;") {
				line += "."
			}
		}
		parts = append(parts, line)
	}
	text := strings.Join(strings.Fields(strings.Join(parts, " ")), " ")
	return strings.ReplaceAll(text, "**", "")
}

// limit cuts a claim to the FindingIn limit, in runes, and marks the cut.
func limit(s string) string {
	if utf8.RuneCountInString(s) <= finding.ClaimMaxChars {
		return s
	}
	r := []rune(s)[:finding.ClaimMaxChars-1]
	return strings.TrimRight(string(r), " ") + "…"
}
