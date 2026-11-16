package secretscan

import (
	"strings"
)

// Leak is one kind of thing an agent's room post must not contain (AGENT_INTERACTION_ARCH 10, H11). The matched text
// itself is never kept.
type Leak string

// Leak kinds.
const (
	LeakCredential Leak = "credential" // a token, key or identifier the scanner finds
	LeakCanary     Leak = "canary"     // a planted canary string
	LeakCardText   Leak = "card_text"  // a run of a private card's text
)

// minCardRun is the shortest run of a card's text that counts as leaked: long enough that ordinary words in common do
// not match.
const minCardRun = 40

// Private is what a room post must not repeat.
type Private struct {
	Canaries  []string
	CardTexts []string
}

// ScanPost returns the kinds of leak in one agent post, each once, in a fixed order.
func ScanPost(text string, scanner *SecretScanner, private Private) ([]Leak, error) {
	var out []Leak
	if scanner != nil {
		if _, found, err := scanner.Scan(text); err != nil {
			return nil, err
		} else if len(found) > 0 {
			out = append(out, LeakCredential)
		}
	}
	for _, c := range private.Canaries {
		if c != "" && strings.Contains(text, c) {
			out = append(out, LeakCanary)
			break
		}
	}
	if repeatsCard(text, private.CardTexts) {
		out = append(out, LeakCardText)
	}
	return out, nil
}

// repeatsCard reports whether the post carries any run of minCardRun characters of a card, whitespace collapsed.
func repeatsCard(text string, cards []string) bool {
	post := collapse(text)
	for _, card := range cards {
		c := collapse(card)
		for start := 0; start+minCardRun <= len(c); start += minCardRun / 2 {
			if strings.Contains(post, c[start:start+minCardRun]) {
				return true
			}
		}
	}
	return false
}

func collapse(s string) string { return strings.Join(strings.Fields(strings.ToLower(s)), " ") }
