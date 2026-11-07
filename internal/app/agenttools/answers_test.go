package agenttools

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
)

func askedFor(t *testing.T, ref string, qid string) change.Event {
	t.Helper()
	raw, err := json.Marshal(change.QuestionAsked{QuestionID: change.QuestionID(qid), Refs: []string{ref}, Sender: "sw-1", MessageID: "m-1"})
	require.NoError(t, err)
	return change.Event{Type: change.EventQuestionAsked, Payload: raw}
}

func answeredFor(t *testing.T, qid, by, text string) change.Event {
	t.Helper()
	raw, err := json.Marshal(change.QuestionAnswered{QuestionID: change.QuestionID(qid), Sender: change.SenderID(by), MessageID: "m-2", Answer: text})
	require.NoError(t, err)
	return change.Event{Type: change.EventQuestionAnswered, Payload: raw}
}

// The envelope carries each answered question with its id, the answer and who gave it. A question nobody answered
// yet is not listed; the agent waits for it or is told it was assumed (A14).
func TestCollectAnswersPairsAnsweredQuestionsOnly(t *testing.T) {
	events := []change.Event{
		askedFor(t, "Q1", "q-1"),
		askedFor(t, "Q2", "q-2"),
		answeredFor(t, "q-1", "ana", "use the 409 response"),
	}

	got := collectAnswers(events)

	require.Equal(t, []workflow.AnswerRef{{ID: "Q1", Answer: "use the 409 response", AnsweredBy: "ana"}}, got)
}

func TestCollectAnswersIsEmptyWithoutAnswers(t *testing.T) {
	require.Empty(t, collectAnswers([]change.Event{askedFor(t, "Q1", "q-1")}))
	require.Empty(t, collectAnswers(nil))
}
