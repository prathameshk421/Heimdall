package redact

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Count records how many values a rule replaced. It never contains a match.
type Count struct {
	Kind  string `json:"kind"`
	Rule  string `json:"rule"`
	Count int    `json:"count"`
}

// Report contains only aggregate information about replacements.
type Report struct {
	Counts            []Count `json:"counts"`
	TotalReplacements int     `json:"total_replacements"`
	HadSecrets        bool    `json:"had_secrets"`
	HadPII            bool    `json:"had_pii"`
}

type rule struct {
	kind, name, pattern, placeholder string
}

// Scrub replaces credential and personal-data matches with typed placeholders.
func Scrub(text string) (string, Report) {
	var report Report
	counts := make(map[string]int)
	patterns := []rule{
		{"secret", "private_key", `-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----[\s\S]*?-----END [A-Z0-9 ]*PRIVATE KEY-----`, "[REDACTED:secret:private_key]"},
		{"secret", "authorization", `(?i)(\b(?:Bearer|Basic)\s+)[A-Za-z0-9._~+/=-]{6,}`, ""},
		{"secret", "github_token", `\b(?:github_pat_[A-Za-z0-9_]{10,}|gh[pousr]_[A-Za-z0-9]{20,})\b`, "[REDACTED:secret:github_token]"},
		{"secret", "aws_access_key", `\b(?:AKIA|ASIA)[A-Z0-9]{16}\b`, "[REDACTED:secret:aws_access_key]"},
		{"secret", "openai_anthropic_openrouter_key", `\bsk-(?:ant-|or-v1-)?[A-Za-z0-9_-]{12,}\b`, "[REDACTED:secret:provider_key]"},
		{"secret", "stripe_key", `\b(?:sk|rk)_live_[A-Za-z0-9]{12,}\b`, "[REDACTED:secret:stripe_key]"},
		{"secret", "slack_token", `\bxox(?:b|p|a)-[A-Za-z0-9-]{10,}\b`, "[REDACTED:secret:slack_token]"},
		{"secret", "jwt", `\beyJ[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]{5,}\b`, "[REDACTED:secret:jwt]"},
		{"secret", "url_credentials", `(?i)(https?://)[^:/@\s]+:[^/@\s]+@`, ""},
		{"secret", "credential_assignment", `(?i)(^|[\s,{;=])(["']?)(api[_-]?key|secret|token|password)(["']?)(\s*[:=]\s*)(["']?)([A-Za-z0-9_./+=-]{8,})(["']?)`, ""},
		{"pii", "email", `(?i)\b[A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,}\b`, "[REDACTED:pii:email]"},
		{"pii", "credit_card", `\b(?:\d[ -]?){13,19}\b`, "[REDACTED:pii:credit_card]"},
		{"pii", "ipv4", `\b(?:\d{1,3}\.){3}\d{1,3}\b`, "[REDACTED:pii:ip]"},
		{"pii", "phone", `(^|[^A-Za-z0-9])(?:\+\d{1,3}[ .()-]*)?(?:\(?\d{2,4}\)?[ .-]*)\d{3,4}[ .-]*\d{3,4}\b`, "[REDACTED:pii:phone]"},
		{"pii", "home_user", `(?:/Users/|/home/)[^/\s]+/|[A-Za-z]:\\Users\\[^\\\s]+\\`, ""},
		{"pii", "ssn", `\b\d{3}-\d{2}-\d{4}\b`, "[REDACTED:pii:ssn]"},
	}
	result := text
	for _, item := range patterns {
		re, err := regexp.Compile(item.pattern)
		if err != nil {
			// These patterns are source constants. Fail closed if a future edit makes one invalid.
			return "[REDACTED:secret:detector_error]", Report{Counts: []Count{{Kind: "secret", Rule: "detector_error", Count: 1}}, TotalReplacements: 1, HadSecrets: true}
		}
		count := 0
		result = re.ReplaceAllStringFunc(result, func(match string) string {
			replacement, matched := replacementFor(item, match)
			if matched {
				count++
			}
			return replacement
		})
		if count > 0 {
			counts[item.kind+"\x00"+item.name] += count
		}
	}
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		parts := strings.SplitN(key, "\x00", 2)
		count := counts[key]
		report.Counts = append(report.Counts, Count{Kind: parts[0], Rule: parts[1], Count: count})
		report.TotalReplacements += count
		if parts[0] == "secret" {
			report.HadSecrets = true
		} else {
			report.HadPII = true
		}
	}
	return result, report
}

// ScrubLines scrubs each line and combines its counts in deterministic order.
func ScrubLines(lines []string) ([]string, Report) {
	if len(lines) == 0 {
		return []string{}, Report{}
	}
	clean, report := Scrub(strings.Join(lines, "\n"))
	return strings.Split(clean, "\n"), report
}

// FormatReport renders an aggregate summary without including matched values.
func FormatReport(report Report) string {
	if report.TotalReplacements == 0 {
		return "No sensitive values found."
	}
	parts := make([]string, 0, len(report.Counts))
	for _, item := range report.Counts {
		parts = append(parts, fmt.Sprintf("%s/%s: %d", item.Kind, item.Rule, item.Count))
	}
	return fmt.Sprintf("Redacted %d value(s): %s", report.TotalReplacements, strings.Join(parts, ", "))
}

func replacementFor(item rule, match string) (string, bool) {
	switch item.name {
	case "private_key":
		return item.placeholder + strings.Repeat("\n", strings.Count(match, "\n")), true
	case "authorization":
		prefix, _, _ := strings.Cut(match, " ")
		return prefix + " [REDACTED:secret:authorization]", true
	case "url_credentials":
		return strings.SplitN(match, "://", 2)[0] + "://[REDACTED:secret:url_credentials]@", true
	case "credential_assignment":
		return redactAssignment(match)
	case "phone":
		return redactPhone(item, match)
	case "ipv4":
		return redactIPv4(item, match)
	case "home_user":
		return redactHomeUser(match)
	case "credit_card":
		return redactCard(item, match)
	default:
		return item.placeholder, true
	}
}

func redactAssignment(match string) (string, bool) {
	left := strings.TrimLeft(match, " \t\r\n,{;=\"'")
	prefix := match[:len(match)-len(left)]
	separator := strings.IndexAny(left, ":=")
	if separator < 0 {
		return match, false
	}
	rawValue := left[separator+1:]
	valuePart := strings.TrimLeft(rawValue, " \t\r\n")
	spacePrefix := rawValue[:len(rawValue)-len(valuePart)]
	quote := ""
	if strings.HasPrefix(valuePart, "\"") || strings.HasPrefix(valuePart, "'") {
		quote, valuePart = valuePart[:1], valuePart[1:]
	}
	closingQuote := ""
	if quote != "" && strings.HasSuffix(valuePart, quote) {
		closingQuote, valuePart = quote, strings.TrimSuffix(valuePart, quote)
	}
	if entropyBitsPerByte(valuePart) < 3.5 {
		return match, false
	}
	return prefix + left[:separator+1] + spacePrefix + quote + "[REDACTED:secret:credential_assignment]" + closingQuote, true
}

func redactPhone(item rule, match string) (string, bool) {
	original := match
	prefix := ""
	if len(match) > 0 && !isPhoneStart(match[0]) {
		prefix, match = match[:1], match[1:]
	}
	digits := digitCount(match)
	if digits < 10 || digits > 15 || isIPv4(match) || isDateTime(match) {
		return original, false
	}
	return prefix + item.placeholder, true
}

func isPhoneStart(char byte) bool { return char == '+' || char == '(' || char >= '0' && char <= '9' }

func redactIPv4(item rule, match string) (string, bool) {
	parts := strings.Split(match, ".")
	if len(parts) != 4 {
		return match, false
	}
	var octets [4]int
	for i, part := range parts {
		value, err := strconv.Atoi(part)
		if err != nil || value < 0 || value > 255 {
			return match, false
		}
		octets[i] = value
	}
	if octets[0] == 127 || octets == [4]int{} || isDocumentationIP(octets) {
		return match, false
	}
	return item.placeholder, true
}

func redactHomeUser(match string) (string, bool) {
	if strings.HasPrefix(match, "/Users/") {
		return "/Users/[REDACTED:pii:user]/", true
	}
	if strings.HasPrefix(match, "/home/") {
		return "/home/[REDACTED:pii:user]/", true
	}
	return `C:\Users\[REDACTED:pii:user]\`, true
}

func redactCard(item rule, match string) (string, bool) {
	if !luhnValid(match) {
		return match, false
	}
	return item.placeholder, true
}

func digitCount(value string) int {
	count := 0
	for _, r := range value {
		if r >= '0' && r <= '9' {
			count++
		}
	}
	return count
}

func luhnValid(value string) bool {
	sum, digits, alternate := 0, 0, false
	for i := len(value) - 1; i >= 0; i-- {
		c := value[i]
		if c < '0' || c > '9' {
			continue
		}
		n := int(c - '0')
		if alternate {
			n *= 2
			if n > 9 {
				n -= 9
			}
		}
		sum += n
		digits++
		alternate = !alternate
	}
	return digits >= 13 && digits <= 19 && sum%10 == 0
}

func entropyBitsPerByte(value string) float64 {
	var frequencies [256]int
	for i := 0; i < len(value); i++ {
		frequencies[value[i]]++
	}
	var entropy float64
	for _, count := range frequencies {
		if count == 0 {
			continue
		}
		probability := float64(count) / float64(len(value))
		entropy -= probability * log2(probability)
	}
	return entropy
}

func log2(value float64) float64 {
	exponent := 0
	for value < 1 {
		value *= 2
		exponent--
	}
	fraction := 0.0
	for bit := 1; bit <= 20; bit++ {
		value *= value
		if value >= 2 {
			value /= 2
			fraction += 1 / float64(uint64(1)<<bit)
		}
	}
	return float64(exponent) + fraction
}

func isDateTime(value string) bool {
	return strings.Contains(value, ":") && strings.Count(value, "-") >= 2 && digitCount(value) >= 12
}

func isIPv4(value string) bool {
	parts := strings.Split(value, ".")
	if len(parts) != 4 {
		return false
	}
	for _, part := range parts {
		octet, err := strconv.Atoi(part)
		if err != nil || octet < 0 || octet > 255 {
			return false
		}
	}
	return true
}

func isDocumentationIP(octets [4]int) bool {
	return octets[0] == 192 && octets[1] == 0 && octets[2] == 2 ||
		octets[0] == 198 && octets[1] == 51 && octets[2] == 100 ||
		octets[0] == 203 && octets[1] == 0 && octets[2] == 113
}
