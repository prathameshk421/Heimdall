package commands

import (
	"regexp"
	"strings"
)

// linkCodeRE is the shape of a link code: six letters or digits (D194). Case does not matter.
var linkCodeRE = regexp.MustCompile(`^[A-Za-z0-9]{6}$`)

// LinkCode reads `heimdall link <code>` (`/heimdall link <code>` on GitHub) and returns the code in upper case. Link is
// not a Command: it changes no change, and on GitHub its sender need not be a mapped person yet, so it is read apart
// from Parse.
func LinkCode(in Input) (string, bool) {
	body, ok := commandBody(in)
	if !ok {
		return "", false
	}
	verb, args := cutWord(body)
	if !strings.EqualFold(verb, "link") || !linkCodeRE.MatchString(args) {
		return "", false
	}
	return strings.ToUpper(args), true
}
