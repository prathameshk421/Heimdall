package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
)

// Limits of the questions an agent may ask. The schema repeats them, and the schema tests check that they agree.
const (
	maxQuestions     = 3
	maxQuestionChars = 400
)

// talkTools are the tools of the talk area: questions to people, and the reviewer context packs.
func (s *Server) talkTools() ([]tool, error) {
	tools := []tool{
		{name: "ask_humans", description: "Ask the people on the change up to 3 questions.",
			schema: askHumansSchema(), roles: []workflow.Role{workflow.RoleTestAuthor, workflow.RoleResolver}, handle: s.askHumans},
	}
	if s.deps.Packs != nil {
		tools = append(tools, tool{name: "read_pack", description: "Read your own fresh, verified reviewer context pack. Private canaries are filtered before it is returned.", schema: readPackSchema(), roles: []workflow.Role{workflow.RoleReviewer}, handle: s.readPack})
	}
	if s.deps.PackPublisher != nil {
		tools = append(tools,
			tool{name: "review_pack", description: "Review a redacted pack preview as a mapped owner before publishing it to a reviewer.", schema: packReviewSchema(), roles: []workflow.Role{workflow.RoleReviewer}, handle: s.reviewPack},
			tool{name: "publish_pack", description: "Publish the exact redacted pack preview approved by the mapped owner.", schema: packPublishSchema(), roles: []workflow.Role{workflow.RoleReviewer}, handle: s.publishPack},
		)
	}
	return tools, nil
}

func readPackSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"section":{"type":"string","maxLength":128}},"additionalProperties":false}`)
}

func packReviewSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"repository":{"type":"string","minLength":1,"maxLength":256},"reviewer":{"type":"string","minLength":1,"maxLength":128},"files":{"type":"object","minProperties":1,"maxProperties":128,"additionalProperties":{"type":"string","maxLength":65536}}},"required":["repository","reviewer","files"],"additionalProperties":false}`)
}

func packPublishSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"repository":{"type":"string","minLength":1,"maxLength":256},"reviewer":{"type":"string","minLength":1,"maxLength":128},"replay_key":{"type":"string","minLength":1,"maxLength":128},"review_digest":{"type":"string","pattern":"^[0-9a-f]{64}$"},"files":{"type":"object","minProperties":1,"maxProperties":128,"additionalProperties":{"type":"string","maxLength":65536}}},"required":["repository","reviewer","replay_key","review_digest","files"],"additionalProperties":false}`)
}

func askHumansSchema() json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`{"type":"object","properties":{"questions":{"type":"array","minItems":1,"maxItems":%d,`+
		`"items":{"type":"string","minLength":1,"maxLength":%d}}},"required":["questions"],"additionalProperties":false}`,
		maxQuestions, maxQuestionChars))
}

func (s *Server) readPack(ctx context.Context, p Principal, args json.RawMessage) (string, error) {
	var input struct {
		Section string `json:"section,omitempty"`
	}
	if err := decodeArgs(args, &input); err != nil {
		return "", err
	}
	assignment, err := s.currentTask(ctx, p)
	if err != nil {
		return "", err
	}
	if assignment.Task.Role != workflow.RoleReviewer || p.Role != workflow.RoleReviewer {
		return "", refusal("Only the assigned reviewer can read a context pack.")
	}
	content, err := s.deps.Packs.Read(ctx, p, assignment, input.Section)
	if err != nil {
		return "", fmt.Errorf("read reviewer context pack: %w", err)
	}
	return wrapUntrusted("private reviewer context pack", []byte(content))
}

func (s *Server) reviewPack(ctx context.Context, p Principal, args json.RawMessage) (string, error) {
	var input struct {
		Repository string            `json:"repository"`
		Reviewer   string            `json:"reviewer"`
		Files      map[string]string `json:"files"`
	}
	if err := decodeArgs(args, &input); err != nil {
		return "", err
	}
	files := make(map[string][]byte, len(input.Files))
	for name, content := range input.Files {
		files[name] = []byte(content)
	}
	preview, err := s.deps.PackPublisher.Review(ctx, p, input.Repository, input.Reviewer, files)
	if err != nil {
		return "", fmt.Errorf("review redacted context pack: %w", err)
	}
	return wrapUntrusted("redacted pack review", []byte(preview))
}

func (s *Server) publishPack(ctx context.Context, p Principal, args json.RawMessage) (string, error) {
	var input struct {
		Repository   string            `json:"repository"`
		Reviewer     string            `json:"reviewer"`
		ReplayKey    string            `json:"replay_key"`
		ReviewDigest string            `json:"review_digest"`
		Files        map[string]string `json:"files"`
	}
	if err := decodeArgs(args, &input); err != nil {
		return "", err
	}
	files := make(map[string][]byte, len(input.Files))
	for name, content := range input.Files {
		files[name] = []byte(content)
	}
	id, err := s.deps.PackPublisher.Publish(ctx, p, input.Repository, input.Reviewer, input.ReplayKey, input.ReviewDigest, files)
	if err != nil {
		return "", fmt.Errorf("publish reviewed context pack: %w", err)
	}
	return "Published reviewer context pack " + id + ".", nil
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
