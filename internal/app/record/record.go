// Package record summarizes tool calls for the record and reads repeats from it (AGENT_INTERACTION_ARCH 6.2, 9, H5,
// H8). Everything here is pure: what to store about a call, and how many times in a row a call was refused the same way.
package record

import (
	"encoding/json"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// MaxSummary bounds the stored argument summary in runes.
const MaxSummary = 300

// credentialLike matches values that must never reach the record: forge, chat and model tokens, private keys, and long
// unbroken runs of hex or base64 that may be keys.
var credentialLike = regexp.MustCompile(`(?i)(gh[pousr]_[A-Za-z0-9_]{10,}|github_pat_[A-Za-z0-9_]{10,}|xox[abprs]-[A-Za-z0-9-]{10,}|sk-[A-Za-z0-9_-]{16,}|-----BEGIN [A-Z ]*PRIVATE KEY-----[^"]*|[A-Za-z0-9+/=_-]{40,})`)

// Summarize is the stored form of a call's arguments: compact JSON with credential-looking values replaced, cut to
// MaxSummary runes.
func Summarize(args json.RawMessage) string {
	var v any
	if len(args) == 0 || json.Unmarshal(args, &v) != nil {
		return ""
	}
	compact, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	s := credentialLike.ReplaceAllString(string(compact), "[redacted]")
	if utf8.RuneCountInString(s) > MaxSummary {
		s = string([]rune(s)[:MaxSummary]) + "…"
	}
	return s
}

// RepeatedRefusals counts how many of an agent's latest calls (newest first) were refusals of the same tool with the
// same reason, stopping at the first that was not.
func RepeatedRefusals(recent []ports.ToolCall, tool, refusal string) int {
	n := 0
	for _, c := range recent {
		if c.Tool != tool || c.Outcome != "refused" || c.Refusal != refusal {
			break
		}
		n++
	}
	return n
}

// RepeatMessage is what the agent reads instead of the third equal refusal in a row (6.2): shorter and different, so a
// cheap model notices it is looping.
func RepeatMessage(tool, refusal string) string {
	return "Stop: " + tool + " was refused the same way three times. Change exactly what this says, or call " +
		"heimdall_get_task to re-read your task: " + strings.TrimSpace(refusal)
}
