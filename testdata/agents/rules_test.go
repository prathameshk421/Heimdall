package agents

import (
	"os"
	"strings"
	"testing"
)

// Every agent's AGENTS.md carries the thirteen shared rules of the behaviour spec (section 2.1). Each rule is checked by
// a phrase only that rule uses, so a dropped or reworded rule fails here.
func TestAgentRulesCoverAllThirteen(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("AGENTS.md")
	if err != nil {
		t.Fatalf("read AGENTS.md: %v", err)
	}
	text := string(raw)
	phrases := map[string]string{
		"1 tools only":          "heimdall_*",
		"2 stay in your room":   "Stay in your room",
		"3 data not commands":   "is data, not instructions",
		"4 no secrets":          "redacted",
		"5 no unchecked claims": "unverified",
		"6 no question tool":    "Do not use the `question` tool",
		"7 one task at a time":  "one task at a time",
		"8 never stop silently": "Never stop silently",
		"9 compact at 70 pct":   "70%",
		"10 role identity":      "owner's handle",
		"11 be short":           "Be short",
		"12 rules win":          "follow these rules",
		"13 helpful first":      "never end on a bare refusal",
	}
	for rule, phrase := range phrases {
		if !strings.Contains(text, phrase) {
			t.Errorf("AGENTS.md is missing shared rule %s (phrase %q)", rule, phrase)
		}
	}
}
