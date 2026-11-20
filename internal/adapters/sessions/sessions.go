package sessions

import (
	"time"
	"unicode/utf8"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/cardgen"
)

// Bounds for reading one agent database.
const (
	// MaxTurnChars is the longest text one turn keeps. It matches the Claude Code and Codex readers.
	MaxTurnChars = 4000
	// MaxTurnsPerSession bounds the turns read from one session.
	MaxTurnsPerSession = 5000
)

// Session is one past agent session of the repository, in the shape the Claude Code and Codex readers produce:
// ordered turns, and the pull requests the session produced when the agent records them.
type Session struct {
	Agent   string
	ID      string
	Updated time.Time
	Turns   []cardgen.Turn
	Links   []cardgen.PRLink
}

// Limits tell a reader how much to read: sessions updated before Since are skipped, and at most MaxSessions
// are returned, newest first.
type Limits struct {
	Since       time.Time
	MaxSessions int
}

// CapText cuts s to MaxTurnChars bytes without splitting a character.
func CapText(s string) string {
	if len(s) <= MaxTurnChars {
		return s
	}
	cut := MaxTurnChars
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

// DateOfMillis formats a Unix time in milliseconds as 2006-01-02 in UTC. Zero gives "".
func DateOfMillis(ms int64) string {
	if ms <= 0 {
		return ""
	}
	return time.UnixMilli(ms).UTC().Format(time.DateOnly)
}
