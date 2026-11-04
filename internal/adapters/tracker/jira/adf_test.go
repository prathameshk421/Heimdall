package jira

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHeadingAndOrderedListBecomeMarkdownLines(t *testing.T) {
	raw := `{"type":"doc","version":1,"content":[
		{"type":"heading","attrs":{"level":2},"content":[{"type":"text","text":"Acceptance criteria"}]},
		{"type":"orderedList","content":[
			{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"First."}]}]},
			{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"Second."}]}]}
		]}
	]}`

	got, err := descriptionText(json.RawMessage(raw))

	require.NoError(t, err)
	require.Equal(t, "## Acceptance criteria\n\n1. First.\n2. Second.", got)
}

func TestBulletListNestsItsChildListsWithIndentation(t *testing.T) {
	raw := `{"type":"doc","content":[{"type":"bulletList","content":[
		{"type":"listItem","content":[
			{"type":"paragraph","content":[{"type":"text","text":"Outer"}]},
			{"type":"bulletList","content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"Inner"}]}]}]}
		]}
	]}]}`

	got, err := descriptionText(json.RawMessage(raw))

	require.NoError(t, err)
	require.Equal(t, "- Outer\n  - Inner", got)
}

func TestInlineMarksHardBreaksAndMentionsKeepTheVisibleText(t *testing.T) {
	raw := `{"type":"doc","content":[{"type":"paragraph","content":[
		{"type":"text","text":"Refund "},
		{"type":"text","text":"now","marks":[{"type":"strong"}]},
		{"type":"hardBreak"},
		{"type":"text","text":"fast "},
		{"type":"mention","attrs":{"id":"x","text":"@ana"}}
	]}]}`

	got, err := descriptionText(json.RawMessage(raw))

	require.NoError(t, err)
	require.Equal(t, "Refund now fast @ana", got)
}

func TestBlocksAreSeparatedByABlankLine(t *testing.T) {
	raw := `{"type":"doc","content":[
		{"type":"paragraph","content":[{"type":"text","text":"Intro."}]},
		{"type":"paragraph","content":[{"type":"text","text":"Body."}]}
	]}`

	got, err := descriptionText(json.RawMessage(raw))

	require.NoError(t, err)
	require.Equal(t, "Intro.\n\nBody.", got)
}

func TestAMissingOrNullDescriptionIsEmpty(t *testing.T) {
	for _, raw := range []json.RawMessage{nil, json.RawMessage("null")} {
		got, err := descriptionText(raw)
		require.NoError(t, err)
		require.Empty(t, got)
	}
}

func TestMalformedOrNonADFDescriptionIsRefused(t *testing.T) {
	_, err := descriptionText(json.RawMessage(`{"type":`))
	require.Error(t, err, "invalid JSON")

	_, err = descriptionText(json.RawMessage(`"h2. wiki markup"`))
	require.Error(t, err, "REST v2 wiki markup is not ADF (V52); refuse it rather than guess")
}

// FuzzDescriptionText proves the walker never panics on arbitrary input, and that everything it accepts is an ADF
// document (or a blank/null description). The seeds include malformed JSON, a non-doc root, a wiki-markup string and
// a real KAN-shaped document, so the fuzzer starts from the shapes the adapter actually meets.
func FuzzDescriptionText(f *testing.F) {
	seeds := []string{
		"",
		"null",
		`{"type":`,
		`"h2. wiki markup"`,
		`{"type":"paragraph","content":[{"type":"text","text":"not a doc root"}]}`,
		`{"type":"doc","version":1,"content":[
			{"type":"heading","attrs":{"level":2},"content":[{"type":"text","text":"Acceptance criteria"}]},
			{"type":"orderedList","content":[{"type":"listItem","content":[
				{"type":"paragraph","content":[{"type":"text","text":"A refund cannot exceed the payment."}]}]}]}
		]}`,
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		got, err := descriptionText(json.RawMessage(raw))
		if err != nil {
			return
		}
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" || trimmed == "null" {
			if got != "" {
				t.Fatalf("a blank description rendered %q", got)
			}
			return
		}
		var probe struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal([]byte(trimmed), &probe); err != nil {
			t.Fatalf("accepted input that is not JSON: %q: %v", raw, err)
		}
		if probe.Type != "doc" {
			t.Fatalf("accepted a %q root, not a doc: %q", probe.Type, raw)
		}
		if got != strings.TrimSpace(got) {
			t.Fatalf("rendered text is not trimmed: %q", got)
		}
	})
}
