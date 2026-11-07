package main

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
)

func askedEvent(t *testing.T, ref string, qid string) change.Event {
	t.Helper()
	raw, err := json.Marshal(change.QuestionAsked{QuestionID: change.QuestionID(qid), Refs: []string{ref}, Sender: "sw-1", MessageID: "msg-1"})
	require.NoError(t, err)
	return change.Event{Type: change.EventQuestionAsked, Payload: raw}
}

// An answer names an open question of this change. An unknown question, and a second answer to the same question, are
// refused, so an answer cannot land on the wrong question or overwrite one (A14).
func TestAnswerPayloadNamesAnOpenQuestion(t *testing.T) {
	state := []change.Event{askedEvent(t, "Q1", "q-1"), askedEvent(t, "Q2", "q-2")}

	got, err := answerPayload(state, "Q2", "ana", "use the 409 response", "msg-9")
	require.NoError(t, err)
	require.Equal(t, change.QuestionID("q-2"), got.QuestionID)
	require.Equal(t, change.SenderID("ana"), got.Sender)
	require.Equal(t, "use the 409 response", got.Answer)

	_, err = answerPayload(state, "Q9", "ana", "x", "msg-9")
	require.ErrorContains(t, err, "no question Q9")

	answered := append(append([]change.Event(nil), state...), mustAnswered(t, "q-2"))
	_, err = answerPayload(answered, "Q2", "ana", "again", "msg-10")
	require.ErrorContains(t, err, "already answered")
}

func mustAnswered(t *testing.T, qid string) change.Event {
	t.Helper()
	raw, err := json.Marshal(change.QuestionAnswered{QuestionID: change.QuestionID(qid), Sender: "ana", MessageID: "msg-8", Answer: "yes"})
	require.NoError(t, err)
	return change.Event{Type: change.EventQuestionAnswered, Payload: raw}
}

func assignedEvent(t *testing.T, agent string) change.Event {
	t.Helper()
	raw, err := json.Marshal(change.TaskAssigned{Agent: id.AgentName(agent), Role: workflow.RoleTestAuthor})
	require.NoError(t, err)
	return change.Event{Type: change.EventTaskAssigned, Payload: raw}
}

func roomOpenedEvent(t *testing.T, room string) change.Event {
	t.Helper()
	raw, err := json.Marshal(change.RoomOpened{RoomID: change.RoomID(room)})
	require.NoError(t, err)
	return change.Event{Type: change.EventRoomOpened, Payload: raw}
}

// An answer wakes the agent that holds the change's latest task, in the change's room, with a message that names the
// agent and tells it to read the task again (A14). Without a task or a room nobody can be woken.
func TestAnswerWakeNamesTheLatestTaskAgentInTheRoom(t *testing.T) {
	state := []change.Event{roomOpenedEvent(t, "room-7"), assignedEvent(t, "test-author"), assignedEvent(t, "reviewer-2")}

	agent, room, ok := answerWake(state)
	require.True(t, ok)
	require.Equal(t, id.AgentName("reviewer-2"), agent)
	require.Equal(t, change.RoomID("room-7"), room)
	require.Equal(t, "@reviewer-2 Q2 was answered. Call heimdall_get_task.", wakeText(agent, "Q2"))

	_, _, ok = answerWake([]change.Event{roomOpenedEvent(t, "room-7")})
	require.False(t, ok, "no task, nobody to wake")
	_, _, ok = answerWake([]change.Event{assignedEvent(t, "test-author")})
	require.False(t, ok, "no room, nowhere to post")
}
