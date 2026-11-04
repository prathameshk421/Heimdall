package record

import "regexp"

const redacted = "[REDACTED]"

// rule is one scrubbing step. Order matters: whole credentials go before the generic field and email rules.
type rule struct {
	re   *regexp.Regexp
	with string
}

func rules() []rule {
	return []rule{
		{regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?-----END [A-Z ]*PRIVATE KEY-----`), redacted},
		{regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*://[^:/\s"@]+:)[^@\s"/]+(@)`), "${1}" + redacted + "${2}"},
		{regexp.MustCompile(`eyJ[A-Za-z0-9_-]{8,}\.eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}`), redacted},
		{regexp.MustCompile(`github_pat_[A-Za-z0-9_]{20,}`), redacted},
		{regexp.MustCompile(`gh[pousr]_[A-Za-z0-9]{20,}`), redacted},
		{regexp.MustCompile(`sk-[A-Za-z0-9_-]{20,}`), redacted},
		{regexp.MustCompile(`ATATT[A-Za-z0-9_=-]{20,}`), redacted},
		{regexp.MustCompile(`AKIA[0-9A-Z]{16}`), redacted},
		{regexp.MustCompile(`xox[abprs]-[A-Za-z0-9-]{10,}`), redacted},
		{regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._~+/=-]{8,}`), "Bearer " + redacted},
		{regexp.MustCompile(`(?i)("(?:password|passwd|secret|client_secret|api_key|apikey|access_token|refresh_token|token|private_key|authorization)"\s*:\s*)"(?:[^"\\]|\\.)*"`), `${1}"` + redacted + `"`},
		{regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`), "redacted@example.invalid"},
	}
}

// Scrub returns in with secrets and personal data replaced by placeholders. It is idempotent, so a payload
// can be scrubbed again safely.
func Scrub(in []byte) []byte {
	out := in
	for _, r := range rules() {
		out = r.re.ReplaceAll(out, []byte(r.with))
	}
	return out
}
