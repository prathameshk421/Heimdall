package pack

import (
	"fmt"
	"math"
	"regexp"
	"strings"
	"unicode/utf8"
)

var (
	credentialAssignment = regexp.MustCompile(`(?i)(\b(?:credential|password|token|api[_-]?key)\s*[=:]\s*)([^\s,;]+)`)
	credentialHit        = regexp.MustCompile(`(?i)\b(?:credential|password|token|api[_-]?key)\s*[=:]\s*[^\s,;]+|\bauthorization\s*:\s*bearer\s+[^\s,;]+`)
	unresolvedBearerHit  = regexp.MustCompile(`(?im)\bauthorization\s*:\s*bearer\s*(?:$|[;,])`)
	redactedMarker       = regexp.MustCompile(`(?i)\[REDACTED(?::[A-Z_]+)?\]`)
	awsKey               = regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`)
	jwt                  = regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]{4,}\b`)
	privateKey           = regexp.MustCompile(`(?s)-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----.*?-----END [A-Z0-9 ]*PRIVATE KEY-----`)
	connection           = regexp.MustCompile(`(?i)\b(?:postgres(?:ql)?|mysql|mongodb(?:\+srv)?|redis|amqps?)://[^\s"'<>]+`)
	envAssignment        = regexp.MustCompile(`(?im)^\s*(?:AWS_[A-Z0-9_]*(?:KEY|TOKEN|SECRET)[A-Z0-9_]*|(?:OPENAI|GITHUB|STRIPE|SLACK)_?[A-Z0-9_]*(?:KEY|TOKEN|SECRET))\s*=\s*[^\s#]+`)
	email                = regexp.MustCompile(`\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}\b`)
	phone                = regexp.MustCompile(`\+?\d[\d ().-]{7,}\d`)
	card                 = regexp.MustCompile(`(?:\d[ -]?){13,19}`)
	govID                = regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`)
	host                 = regexp.MustCompile(`(?i)\b(?:[a-z0-9-]+\.)+(?:internal|local|corp|private|intranet)\b`)
)

// RedactConfig supplies deployment-specific hostnames and literal deny-list terms.
type RedactConfig struct {
	InternalHostnames []string
	DenyList          []string
	// EntropyMinLength and EntropyThreshold configure token-level Shannon entropy.
	// Zero values select conservative defaults (16 runes and 3.5 bits/rune).
	EntropyMinLength int
	EntropyThreshold float64
}

// Redact applies the default detector set and refuses output with any remaining hit.
func Redact(input string) (string, error) { return RedactWithConfig(input, RedactConfig{}) }

// RedactWithConfig applies the complete detector set, then performs a fail-closed scan.
func RedactWithConfig(input string, cfg RedactConfig) (string, error) {
	out := credentialAssignment.ReplaceAllString(input, `${1}[REDACTED:CREDENTIAL]`)
	patterns := []struct {
		re     *regexp.Regexp
		marker string
	}{
		{regexp.MustCompile(`(?i)(authorization\s*:\s*bearer\s+)[^\s,;]+`), `${1}[REDACTED:CREDENTIAL]`},
		{awsKey, "[REDACTED:SECRET]"}, {jwt, "[REDACTED:JWT]"}, {privateKey, "[REDACTED:PRIVATE_KEY]"},
		{connection, "[REDACTED:CONNECTION]"}, {envAssignment, "[REDACTED:ENV]"}, {email, "[REDACTED:EMAIL]"},
		{phone, "[REDACTED:PHONE]"}, {govID, "[REDACTED:GOV_ID]"},
	}
	for _, p := range patterns {
		out = p.re.ReplaceAllString(out, p.marker)
	}
	out = redactCardsAndConfiguredTerms(out, cfg)
	minLength, threshold := cfg.EntropyMinLength, cfg.EntropyThreshold
	if minLength <= 0 {
		minLength = 16
	}
	if threshold <= 0 {
		threshold = 3.5
	}
	out = redactEntropyTokens(out, minLength, threshold)
	out = host.ReplaceAllString(out, "[REDACTED:HOST]")
	check := redactedMarker.ReplaceAllString(out, "")
	if credentialAssignment.MatchString(check) || unresolvedBearerHit.MatchString(out) || privateKey.MatchString(check) || connection.MatchString(check) || email.MatchString(check) || host.MatchString(check) {
		return "", fmt.Errorf("redact pack: unresolved sensitive content")
	}
	return out, nil
}

func redactCardsAndConfiguredTerms(out string, cfg RedactConfig) string {
	for _, value := range card.FindAllString(out, -1) {
		if luhn(value) {
			out = strings.Replace(out, value, "[REDACTED:CARD]", 1)
		}
	}
	for _, value := range append(append([]string{}, cfg.InternalHostnames...), cfg.DenyList...) {
		if value != "" {
			out = strings.ReplaceAll(out, value, "[REDACTED:TEAM]")
		}
	}
	return out
}

func redactEntropyTokens(input string, minLength int, threshold float64) string {
	return strings.Join(mapEntropy(strings.Fields(input), minLength, threshold), " ")
}

func mapEntropy(tokens []string, minLength int, threshold float64) []string {
	for i, token := range tokens {
		if redactedMarker.MatchString(token) || utf8.RuneCountInString(token) < minLength {
			continue
		}
		// Avoid swallowing punctuation around a candidate; inspect and replace the contiguous token core.
		start, end := 0, len(token)
		for start < end && !isTokenRune(rune(token[start])) {
			start++
		}
		for end > start && !isTokenRune(rune(token[end-1])) {
			end--
		}
		core := token[start:end]
		if utf8.RuneCountInString(core) >= minLength && shannonEntropy(core) >= threshold {
			tokens[i] = token[:start] + "[REDACTED:SECRET]" + token[end:]
		}
	}
	return tokens
}

func isTokenRune(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-'
}

func shannonEntropy(s string) float64 {
	counts := make(map[rune]int)
	n := 0
	for _, r := range s {
		counts[r]++
		n++
	}
	var entropy float64
	for _, count := range counts {
		p := float64(count) / float64(n)
		entropy -= p * math.Log2(p)
	}
	return entropy
}

func luhn(s string) bool {
	sum, count, double := 0, 0, false
	for i := len(s) - 1; i >= 0; i-- {
		c := s[i]
		if c < '0' || c > '9' {
			continue
		}
		n := int(c - '0')
		if double {
			n *= 2
			if n > 9 {
				n -= 9
			}
		}
		sum += n
		count++
		double = !double
	}
	return count >= 13 && count <= 19 && sum%10 == 0
}
