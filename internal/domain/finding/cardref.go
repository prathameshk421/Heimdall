package finding

import "strings"

// CardPrefix marks a context ref that names a rule card. Such a ref says which rule led to a finding. It grants no tier.
const CardPrefix = "card:"

// CardRef reports whether ref names a rule card. The name is empty when the ref is not well formed: it must be a plain
// file name with no path part, at most MaxRefBytes long.
func CardRef(ref string) (name string, isCard bool) {
	rest, ok := strings.CutPrefix(ref, CardPrefix)
	if !ok {
		return "", false
	}
	rest = strings.TrimSpace(rest)
	if rest == "" || len(ref) > MaxRefBytes || strings.ContainsAny(rest, "/\\\x00 ") || strings.HasPrefix(rest, ".") {
		return "", true
	}
	return rest, true
}
