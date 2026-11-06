package commands

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// answer gives a person's answer to one question the agent asked (A14, spec 4.2). The question is named by its short
// id, Q1, Q2 and so on, the same id the room shows.
func TestParseAnswer(t *testing.T) {
	res := Parse(chat("heimdall answer Q1 use the 409 response for a repeat key"), testPeople)
	require.Equal(t, KindCommand, res.Kind)
	require.Equal(t, Answer{Question: "Q1", Text: "use the 409 response for a repeat key"}, res.Command)
}

func TestParseAnswerRefusesABadQuestionOrNoText(t *testing.T) {
	for _, text := range []string{"heimdall answer", "heimdall answer Q1", "heimdall answer 1 yes", "heimdall answer X9 yes"} {
		res := Parse(chat(text), testPeople)
		require.Equal(t, KindBadArgs, res.Kind, text)
		require.Contains(t, res.Reply, "answer <Q>", text)
	}
}
