package rulecard

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode"
)

// ExportTarget names a local coding-agent rule file format supported by ExportMarkdown.
type ExportTarget string

const (
	// TargetAgentsMD exports into AGENTS.md using idempotent fenced markers.
	TargetAgentsMD ExportTarget = "agents"
	// TargetClaudeMD exports into CLAUDE.md using idempotent fenced markers.
	TargetClaudeMD ExportTarget = "claude"
	// TargetCursorMDC exports a standalone .cursor/rules/heimdall.mdc rule file with MDC frontmatter.
	TargetCursorMDC ExportTarget = "cursor"

	exportBeginMarker = "<!-- heimdall:cards:begin -->"
	exportEndMarker   = "<!-- heimdall:cards:end -->"
	maxImportedCards  = 64
)

var (
	importPathPattern = regexp.MustCompile("`([A-Za-z0-9._/-]+\\*?[A-Za-z0-9._/*-]*)`")
	slugNonAlphaNum   = regexp.MustCompile(`[^a-z0-9]+`)
)

// DefaultExportPath returns the conventional repository-relative path for target.
func DefaultExportPath(target ExportTarget) (string, error) {
	switch target {
	case TargetAgentsMD:
		return "AGENTS.md", nil
	case TargetClaudeMD:
		return "CLAUDE.md", nil
	case TargetCursorMDC:
		return ".cursor/rules/heimdall.mdc", nil
	default:
		return "", fmt.Errorf("unknown export target %q (want agents, claude, or cursor)", target)
	}
}

// ExportMarkdown renders active/approved cards into target's format. For AGENTS.md and CLAUDE.md it
// preserves all human-authored text outside the Heimdall fenced markers, replacing only the managed
// block idempotently.
func ExportMarkdown(target ExportTarget, cards []Card, existing string) (string, error) {
	if _, err := DefaultExportPath(target); err != nil {
		return "", err
	}
	shareable := exportableCards(cards)
	if len(shareable) == 0 {
		return "", errors.New("no active or approved cards to export")
	}
	block := renderExportBlock(shareable)
	if target == TargetCursorMDC {
		return renderCursorMDC(shareable, block), nil
	}
	return spliceFencedBlock(existing, block), nil
}

func exportableCards(cards []Card) []Card {
	var out []Card
	for _, c := range cards {
		if (c.Status == StatusActive || c.Status == StatusApproved) && c.Validate() == nil {
			out = append(out, c)
		}
	}
	slices.SortStableFunc(out, func(a, b Card) int {
		if n := strings.Compare(a.Kind, b.Kind); n != 0 {
			return n
		}
		return strings.Compare(a.Name, b.Name)
	})
	return out
}

func renderExportBlock(cards []Card) string {
	var b strings.Builder
	b.WriteString(exportBeginMarker + "\n")
	b.WriteString("## Heimdall Approved Rule Cards\n\n")
	b.WriteString("Managed by `heimdall cards export`. Edit cards via `heimdall cards`, not inside this block.\n\n")
	for _, c := range cards {
		writeExportCard(&b, c)
	}
	b.WriteString(exportEndMarker)
	return b.String()
}

func writeExportCard(b *strings.Builder, c Card) {
	title := strings.TrimSuffix(stripFormatControls(c.Name), ".md")
	kind := c.Kind
	if kind == "" {
		kind = KindConvention
	}
	fmt.Fprintf(b, "### %s (%s)\n", title, kind)
	if len(c.AppliesTo) > 0 {
		fmt.Fprintf(b, "- **Applies to:** `%s`\n", strings.Join(c.AppliesTo, "`, `"))
	}
	if check := strings.TrimSpace(stripFormatControls(c.Check)); check != "" {
		fmt.Fprintf(b, "- **Check:** %s\n", check)
	}
	if proof := strings.TrimSpace(stripFormatControls(c.Proof)); proof != "" {
		fmt.Fprintf(b, "- **Proof:** %s\n", proof)
	}
	if body := strings.TrimSpace(stripFormatControls(c.Body)); body != "" {
		b.WriteString("\n" + body + "\n")
	}
	b.WriteString("\n")
}

func renderCursorMDC(cards []Card, block string) string {
	var globs []string
	for _, c := range cards {
		globs = append(globs, c.AppliesTo...)
	}
	slices.Sort(globs)
	globs = slices.Compact(globs)
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("description: Heimdall approved engineering rule cards\n")
	if len(globs) > 0 {
		fmt.Fprintf(&b, "globs: %s\n", strings.Join(globs, ", "))
		b.WriteString("alwaysApply: false\n")
	} else {
		b.WriteString("alwaysApply: true\n")
	}
	b.WriteString("---\n\n")
	b.WriteString(block)
	b.WriteString("\n")
	return b.String()
}

func spliceFencedBlock(existing, block string) string {
	start := strings.Index(existing, exportBeginMarker)
	end := strings.Index(existing, exportEndMarker)
	if start >= 0 && end > start {
		after := existing[end+len(exportEndMarker):]
		return existing[:start] + block + after
	}
	trimmed := strings.TrimRight(existing, "\r\n")
	if trimmed == "" {
		return block + "\n"
	}
	return trimmed + "\n\n" + block + "\n"
}

// ImportMarkdown parses bulleted or headed rules from AGENTS.md, CLAUDE.md, .cursorrules, or
// user-pasted text into StatusCandidate cards ready for local redaction and human approval.
func ImportMarkdown(source, content string) ([]Card, error) {
	cleaned := strings.TrimSpace(stripFormatControls(content))
	if cleaned == "" {
		return nil, errors.New("import content is empty")
	}
	if strings.TrimSpace(source) == "" {
		source = "pasted-rules"
	}
	rawRules := extractRawRules(cleaned)
	if len(rawRules) == 0 {
		return nil, errors.New("no rule items found in imported content")
	}
	return buildImportedCards(source, rawRules), nil
}

func extractRawRules(content string) []string {
	var rules []string
	var current strings.Builder
	flush := func() {
		text := strings.TrimSpace(current.String())
		if text != "" {
			rules = append(rules, text)
		}
		current.Reset()
	}
	inFence := false
	for line := range strings.SplitSeq(content, "\n") {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "```") {
			inFence = !inFence
			continue
		}
		if inFence || trim == "" || strings.HasPrefix(trim, "<!--") {
			continue
		}
		if strings.HasPrefix(trim, "#") {
			flush()
			continue
		}
		if item, ok := stripBulletPrefix(trim); ok {
			flush()
			current.WriteString(item)
			continue
		}
		if current.Len() > 0 {
			current.WriteByte(' ')
		}
		current.WriteString(trim)
	}
	flush()
	return rules
}

func stripBulletPrefix(line string) (string, bool) {
	for _, prefix := range []string{"- ", "* ", "+ "} {
		if rest, ok := strings.CutPrefix(line, prefix); ok {
			return strings.TrimSpace(rest), true
		}
	}
	if dot := strings.Index(line, ". "); dot > 0 && dot <= 3 {
		allDigits := true
		for i := range dot {
			if line[i] < '0' || line[i] > '9' {
				allDigits = false
				break
			}
		}
		if allDigits {
			return strings.TrimSpace(line[dot+2:]), true
		}
	}
	return "", false
}

func buildImportedCards(source string, rules []string) []Card {
	used := map[string]int{}
	out := make([]Card, 0, min(len(rules), maxImportedCards))
	for _, text := range rules {
		if len(out) == maxImportedCards {
			break
		}
		slug := uniqueSlug(slugifyRule(text), used)
		check := text
		if len(check) > MaxCheckChars {
			check = check[:MaxCheckChars]
		}
		out = append(out, Card{
			Name:      slug + ".md",
			Body:      text,
			AppliesTo: inferGlobs(text),
			Check:     check,
			Kind:      KindConvention,
			Source:    source,
			Status:    StatusCandidate,
			Scope:     ScopePerson,
		})
	}
	return out
}

func inferGlobs(text string) []string {
	matches := importPathPattern.FindAllStringSubmatch(text, 8)
	var globs []string
	for _, m := range matches {
		candidate := strings.TrimSpace(m[1])
		if !strings.Contains(candidate, "/") && !strings.Contains(candidate, "*") {
			continue
		}
		if strings.HasSuffix(candidate, "/") {
			candidate += "**"
		}
		if !slices.Contains(globs, candidate) {
			globs = append(globs, candidate)
		}
	}
	return globs
}

func slugifyRule(text string) string {
	lower := strings.ToLower(text)
	slug := strings.Trim(slugNonAlphaNum.ReplaceAllString(lower, "-"), "-")
	if len(slug) > 40 {
		slug = strings.TrimRight(slug[:40], "-")
	}
	if slug == "" {
		return "rule"
	}
	return slug
}

func uniqueSlug(base string, used map[string]int) string {
	used[base]++
	if used[base] == 1 {
		return base
	}
	return fmt.Sprintf("%s-%d", base, used[base])
}

func stripFormatControls(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, s)
}
