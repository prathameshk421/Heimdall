package main

import (
	"encoding/json"
	"fmt"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/commands"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

// answerPayload builds the question.answered for a person's answer to question ref (Q1, Q2) of this change. The
// question must have been asked, and no answer may be stored for it yet (A14).
func answerPayload(state []change.Event, ref string, person string, text string, messageID string) (change.QuestionAnswered, error) {
	asked := map[string]change.QuestionID{}
	answered := map[change.QuestionID]bool{}
	for _, ev := range state {
		if ev.Type == change.EventQuestionAsked {
			var p change.QuestionAsked
			if err := json.Unmarshal(ev.Payload, &p); err != nil {
				return change.QuestionAnswered{}, fmt.Errorf("read question.asked: %w", err)
			}
			for _, r := range p.Refs {
				asked[r] = p.QuestionID
			}
		}
		if ev.Type == change.EventQuestionAnswered {
			var p change.QuestionAnswered
			if err := json.Unmarshal(ev.Payload, &p); err != nil {
				return change.QuestionAnswered{}, fmt.Errorf("read question.answered: %w", err)
			}
			answered[p.QuestionID] = true
		}
	}
	qid, ok := asked[ref]
	if !ok {
		return change.QuestionAnswered{}, fmt.Errorf("no question %s on this change", ref)
	}
	if answered[qid] {
		return change.QuestionAnswered{}, fmt.Errorf("question %s is already answered", ref)
	}
	return change.QuestionAnswered{QuestionID: qid, Sender: change.SenderID(person), MessageID: change.MessageID(messageID), Answer: text}, nil
}

// answerEvents records a person's answer to a question of the change (A14). The answer is data for the agent.
func (r *commandRuntime) answerEvents(cid id.ChangeID, state []change.Event, person string, source commands.Source, sourceRef string, actor change.Actor, cmd commands.Answer) ([]change.Event, error) {
	payload, err := answerPayload(state, cmd.Question, person, cmd.Text, sourceRef)
	if err != nil {
		return nil, err
	}
	ev, err := r.event(cid, actor, source, change.EventQuestionAnswered, payload, sourceRef)
	if err != nil {
		return nil, err
	}
	return []change.Event{ev}, nil
}
