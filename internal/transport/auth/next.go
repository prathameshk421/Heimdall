package auth

import (
	"net/url"
	"strings"
)

// maxNext bounds the return path a sign-in keeps.
const maxNext = 2048

// safeNext returns raw when it is a path on this server, and "/" otherwise. It refuses anything a browser could
// read as another origin: a scheme, a host, a leading // or /\, any backslash, control character or fragment, and
// the gate's own /auth paths. The value it returns is used as the Location header verbatim.
func safeNext(raw string) string {
	if !localShape(raw) {
		return "/"
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "" || u.Host != "" || u.User != nil || u.Opaque != "" {
		return "/"
	}
	if u.Path == "/auth" || strings.HasPrefix(u.Path, "/auth/") {
		return "/"
	}
	return raw
}

// localShape checks the text before it is parsed: one leading slash, not two, and no byte a browser could read as
// the start of another origin or another header.
func localShape(raw string) bool {
	if raw == "" || len(raw) > maxNext || raw[0] != '/' {
		return false
	}
	if len(raw) > 1 && (raw[1] == '/' || raw[1] == '\\') {
		return false
	}
	return !strings.ContainsFunc(raw, func(c rune) bool { return c < 0x20 || c == 0x7f || c == '\\' || c == '#' })
}
