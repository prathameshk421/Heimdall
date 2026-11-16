package change

// The talk area of the event catalog (TECH_SPEC 3.2): questions to people and their answers.
const (
	EventQuestionAsked    EventType = "question.asked"
	EventQuestionAnswered EventType = "question.answered"
)

// talkPayloads is the registry of the talk area.
func talkPayloads() map[EventType]payloadEntry {
	return map[EventType]payloadEntry{
		EventQuestionAsked:    entry[QuestionAsked](),
		EventQuestionAnswered: entry[QuestionAnswered](),
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
