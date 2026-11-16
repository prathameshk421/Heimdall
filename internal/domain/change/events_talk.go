package change

import "github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"

// The talk area of the event catalog (TECH_SPEC 3.2): questions to people and their answers, and commands an agent
// proposes for a person (AGENT_INTERACTION_ARCH 5.3).
const (
	EventQuestionAsked    EventType = "question.asked"
	EventQuestionAnswered EventType = "question.answered"
	EventCommandProposed  EventType = "command.proposed"
	EventCommandConfirmed EventType = "command.confirmed"
)

// talkPayloads is the registry of the talk area.
func talkPayloads() map[EventType]payloadEntry {
	return map[EventType]payloadEntry{
		EventQuestionAsked:    entry[QuestionAsked](),
		EventQuestionAnswered: entry[QuestionAnswered](),
		EventCommandProposed:  entry[CommandProposed](),
		EventCommandConfirmed: entry[CommandConfirmed](),
	}
}

// QuestionAsked is the payload of question.asked: an agent needs an answer from a person (TECH_SPEC 3.2).
type QuestionAsked struct {
	QuestionID QuestionID `json:"question_id"`
	Refs       []string   `json:"refs"`
	Sender     SenderID   `json:"sender"`
	MessageID  MessageID  `json:"message_id"`
}

// Validate returns every problem with the payload.
func (p QuestionAsked) Validate() error {
	var v problems
	_, err := ParseQuestionID(string(p.QuestionID))
	v.add("question_id", err)
	v.each("refs", p.Refs, checkText)
	_, err = ParseSenderID(string(p.Sender))
	v.add("sender", err)
	_, err = ParseMessageID(string(p.MessageID))
	v.add("message_id", err)
	return v.join()
}

// QuestionAnswered is the payload of question.answered: a person replied in the room.
type QuestionAnswered struct {
	QuestionID QuestionID `json:"question_id"`
	Sender     SenderID   `json:"sender"`
	MessageID  MessageID  `json:"message_id"`
	// Answer is the person's answer text. It is data for the agent, shown inside the untrusted wrapper (A14).
	Answer string `json:"answer"`
}

// Validate returns every problem with the payload.
func (p QuestionAnswered) Validate() error {
	var v problems
	_, err := ParseQuestionID(string(p.QuestionID))
	v.add("question_id", err)
	v.add("answer", checkText(p.Answer))
	_, err = ParseSenderID(string(p.Sender))
	v.add("sender", err)
	_, err = ParseMessageID(string(p.MessageID))
	v.add("message_id", err)
	return v.join()
}

// CommandProposed is the payload of command.proposed: an agent asked to run a deciding command for the person who sent
// the trigger message, and the board echoed it back for that person's yes (5.3). The person, never the agent, is the
// one the command will run as. Trigger is the sender's message the request came from; HeadSHA is the pull request head
// when it was proposed ("" with no pull request), so a push makes the proposal stale.
type CommandProposed struct {
	Trigger    MessageID    `json:"trigger"`
	Sender     SenderID     `json:"sender"`
	Person     ActorID      `json:"person"`
	Text       string       `json:"text"`
	Verb       string       `json:"verb"`
	ProposedBy id.AgentName `json:"proposed_by"`
	HeadSHA    id.SHA       `json:"head_sha,omitempty"`
}

// Validate returns every problem with the payload.
func (p CommandProposed) Validate() error {
	var v problems
	_, err := ParseMessageID(string(p.Trigger))
	v.add("trigger", err)
	_, err = ParseSenderID(string(p.Sender))
	v.add("sender", err)
	v.add("person", checkActor(p.Person))
	v.add("text", checkText(p.Text))
	v.add("verb", checkText(p.Verb))
	v.add("proposed_by", checkAgent(p.ProposedBy))
	if p.HeadSHA != "" {
		v.add("head_sha", checkSHA(p.HeadSHA))
	}
	return v.join()
}

// CommandConfirmed is the payload of command.confirmed: the same sender replied yes to a held proposal.
type CommandConfirmed struct {
	Trigger   MessageID `json:"trigger"`
	Sender    SenderID  `json:"sender"`
	MessageID MessageID `json:"message_id"`
}

// Validate returns every problem with the payload.
func (p CommandConfirmed) Validate() error {
	var v problems
	_, err := ParseMessageID(string(p.Trigger))
	v.add("trigger", err)
	_, err = ParseSenderID(string(p.Sender))
	v.add("sender", err)
	_, err = ParseMessageID(string(p.MessageID))
	v.add("message_id", err)
	return v.join()
}
