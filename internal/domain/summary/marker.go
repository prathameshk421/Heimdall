package summary

import (
	"strconv"
	"strings"
)

// markerPrefix opens the hidden first line of the comment. The adapter finds its own comment by this line and by the
// author login of the comment. No decision reads the line: it is a pointer for display (ACTION_MODE 2.7).
const markerPrefix = "<!-- heimdall:proof-summary "

const markerSuffix = " -->"

// Marker is what the hidden first line holds: the change (the pull request) the comment is about, how many times the
// summary was written, and the head commit it describes.
type Marker struct {
	Change  string
	Version int
	Head    string
}

// String is the marker line. A value that is not safe to put in an HTML comment is written as "unknown", so no input
// can end the comment early or add a field.
func (m Marker) String() string {
	return markerPrefix + "change=" + token(m.Change) + " version=" + strconv.Itoa(max(m.Version, 0)) +
		" head=" + hexToken(m.Head) + markerSuffix
}

// ParseMarker reads the marker from the first line of a comment body. It returns false when the first line is not
// exactly a marker. A marker on any later line does not count, so text a pull request author puts in a comment of
// their own cannot stand in for it.
func ParseMarker(body string) (Marker, bool) {
	line, _, _ := strings.Cut(body, "\n")
	line = strings.TrimSuffix(line, "\r")
	rest, ok := strings.CutPrefix(line, markerPrefix)
	if !ok {
		return Marker{}, false
	}
	rest, ok = strings.CutSuffix(rest, markerSuffix)
	if !ok {
		return Marker{}, false
	}
	parts := strings.Split(rest, " ")
	if len(parts) != 3 {
		return Marker{}, false
	}
	change, ok1 := strings.CutPrefix(parts[0], "change=")
	version, ok2 := strings.CutPrefix(parts[1], "version=")
	head, ok3 := strings.CutPrefix(parts[2], "head=")
	n, err := strconv.Atoi(version)
	if !ok1 || !ok2 || !ok3 || err != nil || n < 0 || version != strconv.Itoa(n) ||
		change != token(change) || head != hexToken(head) {
		return Marker{}, false
	}
	return Marker{Change: change, Version: n, Head: head}, true
}

// token returns s when it is 1 to 64 characters of letters, digits, dot, underscore and hyphen, and "unknown" otherwise.
func token(s string) string {
	if s == "" || len(s) > 64 {
		return "unknown"
	}
	for _, r := range s {
		if !tokenRune(r) {
			return "unknown"
		}
	}
	return s
}

func tokenRune(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._-", r)
}

// hexToken returns s when it is 7 to 64 lower case hex digits (a commit id), and "unknown" otherwise.
func hexToken(s string) string {
	if len(s) < 7 || len(s) > 64 {
		return "unknown"
	}
	for _, r := range s {
		if r < '0' || r > '9' && r < 'a' || r > 'f' {
			return "unknown"
		}
	}
	return s
}

// shortHead is the head commit as a person reads it: the first 7 characters, or "unknown".
func shortHead(h string) string {
	h = hexToken(h)
	if h == "unknown" {
		return h
	}
	return h[:7]
}
