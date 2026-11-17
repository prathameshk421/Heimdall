package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Limits of the questions an agent may ask. The schema repeats them, and the schema tests check that they agree.
const (
	maxQuestions     = 3
	maxQuestionChars = 400
)

// talkTools are the tools of the talk area: questions to people.
func (s *Server) talkTools() ([]tool, error) {
	tools := []tool{
		{name: "ask_humans", description: "Send up to 3 questions to the people in the change room; answers arrive in your next get_task. Use it only when a behaviour cannot be decided from the ticket and the code; do not ask what you can read. Example: ask_humans {\"questions\":[\"Is the limit per day or per payment?\"]}",
			schema: askHumansSchema(), handle: s.askHumans},
	}
	return tools, nil
}

func askHumansSchema() json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`{"type":"object","properties":{"questions":{"type":"array","minItems":1,"maxItems":%d,`+
		`"items":{"type":"string","minLength":1,"maxLength":%d}}},"required":["questions"],"additionalProperties":false}`,
		maxQuestions, maxQuestionChars))
}

// askHumans sends up to maxQuestions questions to the people on the change.
func (s *Server) askHumans(ctx context.Context, p Principal, args json.RawMessage) (string, error) {
	var in struct {
		Questions []string `json:"questions"`
	}
	if err := decodeArgs(args, &in); err != nil {
		return "", err
	}
	if n := len(in.Questions); n < 1 || n > maxQuestions {
		return "", refusalf("Send 1 to %d questions, not %d.", maxQuestions, n)
	}
	for i, q := range in.Questions {
		if strings.TrimSpace(q) == "" {
			return "", refusalf("Question %d is blank.", i+1)
		}
		if n := utf8.RuneCountInString(q); n > maxQuestionChars {
			return "", refusalf("Question %d is %d characters. The limit is %d.", i+1, n, maxQuestionChars)
		}
	}
	a, err := s.currentTask(ctx, p)
	if err != nil {
		return "", err
	}
	if err := s.deps.Tasks.AskHumans(ctx, a, in.Questions); err != nil {
		return "", fmt.Errorf("send questions of task %s: %w", a.Task.TaskID, err)
	}
	return "The questions are sent.", nil
}
