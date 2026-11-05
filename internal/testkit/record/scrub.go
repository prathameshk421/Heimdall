package record

import "regexp"

const redacted = "[REDACTED]"

// rule is one scrubbing step. Order matters: whole credentials go before the generic field and email rules, and a
// rule that writes a placeholder must not be undone by a later rule.
type rule struct {
	re   *regexp.Regexp
	with string
}

// secretKeyNames are matched as substrings of a JSON key, so github_token and signing_secret are caught as well as
// token and secret. Over-redacting a key that merely contains one of these words is the accepted cost.
const secretKeyNames = `token|secret|password|passwd|apikey|api_key|authorization|credential|signature|private_key`

func rules() []rule {
	return []rule{
		// PEM and PGP private key blocks, including the armoured form PGP uses.
		{regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY(?: BLOCK)?-----.*?-----END [A-Z ]*PRIVATE KEY(?: BLOCK)?-----`), redacted},
		{regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*://[^:/\s"@]+:)[^@\s"/]+(@)`), "${1}" + redacted + "${2}"},
		{regexp.MustCompile(`eyJ[A-Za-z0-9_-]{8,}\.eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}`), redacted},
		{regexp.MustCompile(`github_pat_[A-Za-z0-9_]{20,}`), redacted},
		{regexp.MustCompile(`gh[pousr]_[A-Za-z0-9]{20,}`), redacted},
		{regexp.MustCompile(`sk-[A-Za-z0-9_-]{20,}`), redacted},
		{regexp.MustCompile(`[sr]k_(?:live|test)_[A-Za-z0-9]{16,}`), redacted},
		{regexp.MustCompile(`ATATT[A-Za-z0-9_=-]{20,}`), redacted},
		{regexp.MustCompile(`(?:AKIA|ASIA)[0-9A-Z]{16}`), redacted},
		// An AWS secret access key is 40 base64-ish characters, the same shape as a git SHA-1 in hex, so it is only
		// redacted next to its key name.
		{regexp.MustCompile(`(?i)((?:aws_?secret_?(?:access_?)?key|secret_?access_?key)["']?\s*[:=]\s*["']?)[A-Za-z0-9/+=]{40}`), "${1}" + redacted},
		{regexp.MustCompile(`AIza[0-9A-Za-z_-]{35}`), redacted},
		{regexp.MustCompile(`npm_[A-Za-z0-9]{36}`), redacted},
		{regexp.MustCompile(`glpat-[A-Za-z0-9_-]{20,}`), redacted},
		{regexp.MustCompile(`xox[abprs]-[A-Za-z0-9-]{10,}`), redacted},
		{regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._~+/=-]{8,}`), "Bearer " + redacted},
		// Basic credentials are only ever base64 after an Authorization name, so prose such as "basic auth" is left alone.
		{regexp.MustCompile(`(?i)(authorization["']?\s*[:=]\s*["']?basic\s+)[A-Za-z0-9+/]{8,}={0,2}`), "${1}" + redacted},
		// Secret parameters in a URL query or a form-encoded body. The name must start the text or follow a separator.
		{regexp.MustCompile(`(?i)(^|[?&;\s"'])((?:access_)?token|key|secret|password|passwd|signature|api_key)=[^&\s"';]+`), "${1}${2}=" + redacted},
		{regexp.MustCompile(`(?i)("[^"]*(?:` + secretKeyNames + `)[^"]*"\s*:\s*)"(?:[^"\\]|\\.)*"`), `${1}"` + redacted + `"`},
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
