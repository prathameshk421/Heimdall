package external_test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/external"
)

const head = "head0001"

// apiComment is the shape GitHub's REST API gives for a pull request comment, plus the "kind" and "outdated"
// keys the caller adds. The goldens in testdata are written in this shape.
type apiComment struct {
	ID                int64                        `json:"id"`
	Kind              string                       `json:"kind"`
	Body              string                       `json:"body"`
	Path              string                       `json:"path"`
	Line              int                          `json:"line"`
	StartLine         int                          `json:"start_line"`
	OriginalLine      int                          `json:"original_line"`
	OriginalStartLine int                          `json:"original_start_line"`
	Side              string                       `json:"side"`
	CommitID          string                       `json:"commit_id"`
	OriginalCommitID  string                       `json:"original_commit_id"`
	Position          int                          `json:"position"`
	Outdated          bool                         `json:"outdated"`
	InReplyToID       int64                        `json:"in_reply_to_id"`
	User              struct{ Login, Type string } `json:"user"`
}

func (a apiComment) comment() external.Comment {
	return external.Comment{
		ID: a.ID, Kind: external.Kind(a.Kind), Body: a.Body, Path: a.Path,
		Line: a.Line, StartLine: a.StartLine, OriginalLine: a.OriginalLine, OriginalStartLine: a.OriginalStartLine,
		Side: a.Side, CommitID: a.CommitID, OriginalCommitID: a.OriginalCommitID, Position: a.Position,
		Outdated: a.Outdated, InReplyTo: a.InReplyToID, UserLogin: a.User.Login, UserType: a.User.Type,
	}
}

func readFile(t *testing.T, path string, v any) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}

func allSources() []string {
	return []string{
		"coderabbitai[bot]", "copilot-pull-request-reviewer[bot]", "cursor[bot]", "greptile-apps[bot]",
		"macroscopeapp[bot]", "chatgpt-codex-connector[bot]", "devin-ai-integration[bot]",
	}
}

func newPR() external.PR { return external.PR{Head: head, Sources: allSources()} }

// inline is a review comment on the head commit, made by CodeRabbit, with a body that passes its marker check.
func inline(id int64, path string, line int) external.Comment {
	return external.Comment{
		ID: id, Path: path, Line: line, OriginalLine: line, Side: "RIGHT", CommitID: head, OriginalCommitID: head,
		Body: "_⚠️ Potential issue_ | _🟡 Minor_\n\n**The value is not checked.**\n\nIt can be empty here.", UserLogin: "coderabbitai[bot]", UserType: "Bot",
	}
}
