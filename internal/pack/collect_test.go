package pack

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCollectMarkdownRefusesTraversalAndReadsOnlyNamedMarkdown(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(root, "docs"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(root, "docs", "adr.md"), []byte("decision"), 0o600))
	docs, err := CollectMarkdown(root, []string{"docs/adr.md"})
	require.NoError(t, err)
	require.Equal(t, "[doc:docs/adr.md]", docs[0].Provenance)
	require.Equal(t, "decision", docs[0].Raw)
	_, err = CollectMarkdown(root, []string{"../outside.md"})
	require.ErrorContains(t, err, "relative .md")
	_, err = CollectMarkdown(root, []string{".env"})
	require.ErrorContains(t, err, "relative .md")
}

func TestCollectClaudeKeepsOnlyUserAndAssistantTextFromThisRepository(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	require.NoError(t, os.Mkdir(repo, 0o700))
	sessions := t.TempDir()
	lines := `{"type":"user","cwd":"` + repo + `","message":{"content":"keep this"}}` + "\n" + `{"type":"assistant","cwd":"` + repo + `","message":{"content":[{"type":"text","text":"also keep"},{"type":"tool_use","name":"read_file"}]}}` + "\n" + `{"type":"user","cwd":"/another/repo","message":{"content":"drop other repo"}}` + "\n" + `{"type":"tool_result","cwd":"` + repo + `","message":{"content":"drop tool output"}}` + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(sessions, "session.jsonl"), []byte(lines), 0o600))
	docs, err := CollectClaude(context.Background(), repo, []string{sessions})
	require.NoError(t, err)
	require.Len(t, docs, 1)
	require.Contains(t, docs[0].Raw, "keep this")
	require.Contains(t, docs[0].Raw, "also keep")
	require.NotContains(t, docs[0].Raw, "drop other repo")
	require.NotContains(t, docs[0].Raw, "read_file")
	require.NotContains(t, docs[0].Raw, "drop tool output")
}

func TestCollectCodexKeepsOnlyTextMessagesFromMatchingSession(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	require.NoError(t, os.Mkdir(repo, 0o700))
	sessions := t.TempDir()
	lines := `{"type":"session_meta","payload":{"cwd":"` + repo + `"}}` + "\n" +
		`{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"keep request"}]}}` + "\n" +
		`{"type":"response_item","payload":{"type":"function_call","name":"tool"}}` + "\n" +
		`{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"keep answer"},{"type":"tool_result","text":"drop tool output"}]}}` + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(sessions, "rollout.jsonl"), []byte(lines), 0o600))
	docs, err := CollectCodex(context.Background(), repo, []string{sessions})
	require.NoError(t, err)
	require.Len(t, docs, 1)
	require.Contains(t, docs[0].Raw, "keep request")
	require.Contains(t, docs[0].Raw, "keep answer")
	require.NotContains(t, docs[0].Raw, "drop tool output")
}

func TestCollectOpenCodeJoinsOnlyTextPartsForThisRepository(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	storage := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(storage, "session"), 0o700))
	require.NoError(t, os.MkdirAll(filepath.Join(storage, "message", "s1"), 0o700))
	require.NoError(t, os.MkdirAll(filepath.Join(storage, "part", "m1"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(storage, "session", "s1.json"), []byte(`{"id":"s1","directory":"`+repo+`"}`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(storage, "message", "s1", "m1.json"), []byte(`{"id":"m1","role":"assistant"}`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(storage, "part", "m1", "p1.json"), []byte(`{"type":"text","text":"keep answer"}`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(storage, "part", "m1", "p2.json"), []byte(`{"type":"tool","text":"drop tool output"}`), 0o600))
	docs, err := CollectOpenCode(context.Background(), storage, repo)
	require.NoError(t, err)
	require.Len(t, docs, 1)
	require.Contains(t, docs[0].Raw, "keep answer")
	require.NotContains(t, docs[0].Raw, "drop tool output")
}
