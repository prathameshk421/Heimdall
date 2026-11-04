package criteria

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// maxCriteria is the last id the ticket model accepts: AC999 (ports.Ticket.Validate).
const maxCriteria = 999

var (
	// reHeading matches a markdown heading. Level and text are captured.
	reHeading = regexp.MustCompile(`^(#{1,6})\s+(.*?)\s*$`)
	// reItem matches a bullet or numbered item. The leading whitespace shows nesting.
	reItem = regexp.MustCompile(`^(\s*)(?:[-*+]|[0-9]{1,3}[.)])\s+(\S.*?)\s*$`)
)

// Parse returns the acceptance criteria in text: every bullet or numbered item under a heading named
// "Acceptance criteria" (case-insensitive, any level, an optional trailing colon). Nested items and wrapped lines
// join their parent with a newline. A blank line ends an item. Ids are AC1..n in order across all such sections,
// and each hash is the SHA-256 of the criterion text (TECH_SPEC D50).
func Parse(text string) ([]ports.Criterion, error) {
	items := collect(text)
	if len(items) > maxCriteria {
		return nil, fmt.Errorf("%d acceptance criteria: the ticket model allows at most %d", len(items), maxCriteria)
	}
	out := make([]ports.Criterion, 0, len(items))
	for i, t := range items {
		out = append(out, ports.Criterion{
			ID:   "AC" + strconv.Itoa(i+1),
			Hash: hashOf(t),
			Text: t,
		})
	}
	return out, nil
}

// collect walks the lines once and returns the text of every criterion in order.
func collect(text string) []string {
	var items []string
	inSection := false
	current := -1
	for _, line := range strings.Split(text, "\n") {
		if m := reHeading.FindStringSubmatch(line); m != nil {
			inSection = isAcceptanceHeading(m[2])
			current = -1
			continue
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			current = -1
			continue
		}
		if !inSection {
			continue
		}
		items, current = step(items, current, line, trimmed)
	}
	return items
}

// step applies one non-heading line inside an acceptance section. It returns the grown items and the index of the
// item that receives continuation lines.
func step(items []string, current int, line, trimmed string) ([]string, int) {
	if m := reItem.FindStringSubmatch(line); m != nil {
		nested := m[1] != ""
		if nested && current >= 0 {
			items[current] += "\n" + m[2]
			return items, current
		}
		return append(items, m[2]), len(items)
	}
	if current >= 0 {
		items[current] += "\n" + trimmed
	}
	return items, current
}

// isAcceptanceHeading reports whether a heading's text names the acceptance criteria section.
func isAcceptanceHeading(text string) bool {
	name := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(text), ":"))
	return strings.EqualFold(name, "acceptance criteria")
}

// hashOf is the SHA-256 of text, hex encoded. An edit that keeps the text keeps the hash.
func hashOf(text string) string {
	h := sha256.Sum256([]byte(text))
	return hex.EncodeToString(h[:])
}
