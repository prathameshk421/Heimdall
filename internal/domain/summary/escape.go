package summary

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// zwsp is a zero width space. It breaks an autolink or a mention without changing how the text looks.
const zwsp = "\u200b"

// clean turns an untrusted string into one plain line of at most limit runes. Every kind of white space and every
// control character becomes one space, bytes that are not UTF-8 become a space, and format characters (bidirectional
// overrides, zero width characters) are removed, so the text cannot reorder what a person reads or hide a marker. A
// cut text ends with "…". It runs before the escape, so an escape sequence is never cut in half.
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

// cut keeps at most limit runes of s, and ends a cut text with "…" so it is plain that text is missing.
func cut(s string, limit int) string {
	if limit < 1 || utf8.RuneCountInString(s) <= limit {
		return s
	}
	runes := []rune(s)
	return string(runes[:limit-1]) + "…"
}

// escape makes one line of untrusted text safe to place in the middle of a line of GitHub Markdown. It does not
// protect text at the start of a line, and every caller puts text after a prefix of its own. The output cannot:
//
//   - open HTML, an image, a link or a comment: "<", ">" and "&" are entities, and brackets and parentheses carry a
//     backslash;
//   - start emphasis, code, strike through, a table cell or math: the characters of those carry a backslash;
//   - mention a person or a team, or make a mail link: "@" is followed by a zero width space;
//   - make an issue reference: "#" before a digit is followed by a zero width space;
//   - make a bare link: "://" and "www." get a zero width space.
func escape(s string) string {
	var b strings.Builder
	runes, rep := []rune(s), replacements()
	for i, r := range runes {
		b.WriteString(escapeRune(runes, rep, i, r))
	}
	return breakWWW(b.String())
}

// escapeRune is the escape of the rune at runes[i].
func escapeRune(runes []rune, rep map[rune]string, i int, r rune) string {
	if s, ok := rep[r]; ok {
		return s
	}
	switch {
	case r == '#':
		if i+1 < len(runes) && runes[i+1] >= '0' && runes[i+1] <= '9' {
			return `\#` + zwsp
		}
		return `\#`
	case r == ':' && strings.HasPrefix(string(runes[i+1:min(len(runes), i+3)]), "//"):
		return ":" + zwsp
	case strings.ContainsRune(backslashed, r):
		return `\` + string(r)
	}
	return string(r)
}

// replacements is a function so that no package-level state can be changed by a caller.
func replacements() map[rune]string {
	return map[rune]string{'&': "&amp;", '<': "&lt;", '>': "&gt;", '@': "@" + zwsp}
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
		last = at
		from = at
	}
	b.WriteString(s[last:])
	return b.String()
}

// text cleans, cuts and escapes one untrusted string for a line of Markdown.
func text(s string, limit int) string { return escape(clean(s, limit)) }

// code cleans and cuts one untrusted string and wraps it in a code span that nothing inside can close. The span
// delimiter is one backtick longer than the longest run inside. It returns "" for an empty string.
func code(s string, limit int) string {
	s = clean(s, limit)
	if s == "" {
		return ""
	}
	run, longest := 0, 0
	for _, r := range s {
		if r == '`' {
			run++
			longest = max(longest, run)
		} else {
			run = 0
		}
	}
	fence := strings.Repeat("`", longest+1)
	pad := ""
	if strings.HasPrefix(s, "`") || strings.HasSuffix(s, "`") {
		pad = " "
	}
	return fence + pad + s + pad + fence
}
