package pack

import (
	"fmt"
	"regexp"
	"strings"
)

// Redaction records a match without retaining the matched value.
type Redaction struct {
	Kind  string `json:"kind"`
	Count int    `json:"count"`
}

// RedactReport replaces credential and direct-identifier patterns with typed placeholders. Matched text is never
// included in the result metadata. Pattern compilation is local to keep rule configuration immutable.
func RedactReport(input string) (string, []Redaction) {
	rules := []struct{ kind, pattern string }{
		{"private_key", `-----BEGIN ([A-Z ]+ )?PRIVATE KEY-----[\s\S]*?-----END ([A-Z ]+ )?PRIVATE KEY-----`},
		{"bearer", `(?i)\bBearer\s+[A-Za-z0-9._~+/=-]{8,}`},
		{"connection_string", `(?i)\b(postgres|postgresql|mysql|mongodb|redis)://[^\s"'<>]+`},
		{"jwt", `\beyJ[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]{5,}\b`},
		{"cloud_key", `\b(AKIA|ASIA)[A-Z0-9]{16}\b`},
		{"email", `(?i)\b[A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,}\b`},
		{"phone", `\b(?:\+?\d[ .()\-]?){9,15}\b`},
		{"government_id", `\b\d{3}-\d{2}-\d{4}\b`},
		{"env_line", `(?m)^\s*(?:export\s+)?[A-Z][A-Z0-9_]*(?:TOKEN|SECRET|PASSWORD|API_KEY|PRIVATE_KEY)[A-Z0-9_]*\s*=.*$`},
		{"high_entropy", `\b[A-Za-z0-9+/=_-]{40,}\b`},
	}
	result := input
	removed := make([]Redaction, 0, len(rules))
	cardPattern := regexp.MustCompile(`\b(?:\d[ -]?){13,19}\b`)
	for _, value := range cardPattern.FindAllString(result, -1) {
		if luhnValid(value) {
			result = strings.Replace(result, value, "[REDACTED:payment_card]", 1)
			removed = append(removed, Redaction{Kind: "payment_card", Count: 1})
		}
	}
	for _, rule := range rules {
		re, err := regexp.Compile(rule.pattern)
		if err != nil { // Patterns are source constants; this is unreachable unless the implementation is broken.
			continue
		}
		count := len(re.FindAllStringIndex(result, -1))
		if count > 0 {
			result = re.ReplaceAllString(result, "[REDACTED:"+rule.kind+"]")
			removed = append(removed, Redaction{Kind: rule.kind, Count: count})
		}
	}
	return result, removed
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

// RequireClean rejects content if the second pass changes it.
func RequireClean(content string) error {
	redacted, hits := RedactReport(content)
	if len(hits) != 0 {
		kinds := make([]string, 0, len(hits))
		for _, hit := range hits {
			kinds = append(kinds, fmt.Sprintf("%s (%d)", hit.Kind, hit.Count))
		}
		return fmt.Errorf("pack contains sensitive material: %s", strings.Join(kinds, ", "))
	}
	if redacted != content {
		return fmt.Errorf("pack redaction failed")
	}
	return nil
}
