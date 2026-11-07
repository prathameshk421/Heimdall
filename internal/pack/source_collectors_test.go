package pack

import (
	"context"
	"strings"
	"testing"
	"testing/fstest"
)

type fakeGit struct{}

func (fakeGit) Log(context.Context, string) ([]GitCommit, error) {
	return []GitCommit{{Hash: "abc1234", Author: "Dev", Subject: "fix parser"}}, nil
}
func (fakeGit) Blame(context.Context, string, string) ([]GitOwnership, error) {
	return []GitOwnership{{Path: "internal/parser.go", Author: "Dev", Lines: 8}}, nil
}

func TestGitCollectorReturnsBoundedOwnershipWithProvenance(t *testing.T) {
	got, err := (GitCollector{Source: fakeGit{}, MaxCommits: 5}).Collect(context.Background(), "repo", "Dev", []string{"internal/parser.go"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || !strings.Contains(got[0].Content+got[1].Content, "abc1234") || !strings.Contains(got[0].Provenance+got[1].Provenance, "git:") {
		t.Fatalf("git collection lacks bounded provenance: %#v", got)
	}
}

func TestTranscriptCollectorKeepsOnlyRepoUserAndAssistantText(t *testing.T) {
	root := NewFSRoot(fstest.MapFS{"session.jsonl": &fstest.MapFile{Data: []byte(`{"type":"user","cwd":"/repo","message":{"content":"please review"}}` + "\n" + `{"type":"assistant","cwd":"/repo","message":{"content":"Parser rejects empty input."}}` + "\n" + `{"type":"tool_result","cwd":"/repo","content":"credential=raw-secret"}` + "\n" + `{"type":"user","cwd":"/elsewhere","content":"other"}`)}})
	got, err := (TranscriptCollector{Root: root, RepoPath: "/repo", Limits: Limits{MaxFiles: 2, MaxBytes: 4096}}).Collect([]string{"session.jsonl"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !strings.Contains(got[0].Content, "please review") || !strings.Contains(got[0].Content, "Parser rejects") || strings.Contains(got[0].Content, "raw-secret") || strings.Contains(got[0].Content, "other") {
		t.Fatalf("unexpected transcript content: %#v", got)
	}
}
