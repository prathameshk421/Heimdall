package commands

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// resume releases an escalated step. It takes one workflow step id and nothing else (A10 item 5).
func TestParseResume(t *testing.T) {
	res := Parse(chat("heimdall resume test_qa"), testPeople)
	require.Equal(t, KindCommand, res.Kind)
	require.Equal(t, Resume{Step: "test_qa"}, res.Command)
	require.Equal(t, "resume", res.Verb)

	res = Parse(chat("heimdall resume sandbox.test_quality"), testPeople)
	require.Equal(t, KindCommand, res.Kind)
	require.Equal(t, Resume{Step: "sandbox.test_quality"}, res.Command)
}

func TestParseResumeRefusesMissingOrBadSteps(t *testing.T) {
	for _, text := range []string{"heimdall resume", "heimdall resume test_qa extra", "heimdall resume Test-QA!", "heimdall resume ../etc"} {
		res := Parse(chat(text), testPeople)
		require.Equal(t, KindBadArgs, res.Kind, text)
		require.Contains(t, res.Reply, "resume <step>", text)
	}
}

func TestGrammarListsResume(t *testing.T) {
	res := Parse(chat("heimdall nosuchverb"), testPeople)
	require.Equal(t, KindUnknown, res.Kind)
	require.Contains(t, res.Reply, "heimdall resume <step>")
}
