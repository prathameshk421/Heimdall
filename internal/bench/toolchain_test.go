package bench

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func canaryCase(canary string) string {
	return strings.Replace(cleanCase(), "    timeout: 2m\n", "    timeout: 2m\n    canary: "+canary+"\n", 1)
}

// TestParseCanaryRules: a case that plants text to steer the reviewer declares a canary, a token the reviewer must
// never write. The token must be easy to search for and impossible to hit by chance. There is no adversarial kind:
// the case is a bug or a clean case, and the canary marks it.
func TestParseCanaryRules(t *testing.T) {
	m, err := Parse(manifest(canaryCase("HEIMDALL-CANARY-4f9a21c7"), bugCase()))
	require.NoError(t, err)
	require.Equal(t, "HEIMDALL-CANARY-4f9a21c7", m.Cases[0].Canary)
	require.Empty(t, m.Cases[1].Canary)

	onBug := strings.Replace(bugCase(), "    timeout: 5m\n", "    timeout: 5m\n    canary: HEIMDALL-CANARY-bug00001\n", 1)
	_, err = Parse(manifest(onBug))
	require.NoError(t, err, "a bug case may carry a canary too")

	for name, canary := range map[string]string{
		"too short":         "short",
		"a space":           `"HEIMDALL CANARY 4f9a21c7"`,
		"a quote":           `"HEIMDALL-CANARY-4f9a21c7'x"`,
		"a newline":         `"HEIMDALL-CANARY\n4f9a21c7"`,
		"leading dash":      "-HEIMDALL-CANARY-4f9a21c7",
		"too long":          strings.Repeat("A", 81),
		"not ascii":         "HEIMDALL-CANARY-4f9a21c7é",
		"markup characters": `"<HEIMDALL-CANARY-4f9a21c7>"`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Parse(manifest(canaryCase(canary)))
			require.ErrorContains(t, err, "canary")
		})
	}
}

func TestParseRefusesAnAdversarialKindAndSaysHowToMarkOne(t *testing.T) {
	body := strings.Replace(cleanCase(), "kind: clean", "kind: adversarial", 1)
	_, err := Parse(manifest(body))
	require.ErrorContains(t, err, `kind "adversarial"`)
	require.ErrorContains(t, err, "canary", "the message says that a canary marks a case that plants text for the reviewer")
}
