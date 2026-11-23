package jira

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDescriptionText(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		raw     string
		want    string
		wantErr bool
	}{
		{
			name: "heading and ordered list",
			raw: `{"type":"doc","version":1,"content":[
				{"type":"heading","attrs":{"level":2},"content":[{"type":"text","text":"Acceptance criteria"}]},
				{"type":"orderedList","content":[
					{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"First."}]}]},
					{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"Second."}]}]}
				]}
			]}`,
			want: "## Acceptance criteria\n\n1. First.\n2. Second.",
		},
		{
			name: "bullet list indentation",
			raw: `{"type":"doc","content":[{"type":"bulletList","content":[
				{"type":"listItem","content":[
					{"type":"paragraph","content":[{"type":"text","text":"Outer"}]},
					{"type":"bulletList","content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"Inner"}]}]}]}
				]}
			]}]}`,
			want: "- Outer\n  - Inner",
		},
		{
			name: "inline marks hard breaks mentions",
			raw: `{"type":"doc","content":[{"type":"paragraph","content":[
				{"type":"text","text":"Refund "},
				{"type":"text","text":"now","marks":[{"type":"strong"}]},
				{"type":"hardBreak"},
				{"type":"text","text":"fast "},
				{"type":"mention","attrs":{"id":"x","text":"@ana"}}
			]}]}`,
			want: "Refund now fast @ana",
		},
		{
			name: "blank line between blocks",
			raw: `{"type":"doc","content":[
				{"type":"paragraph","content":[{"type":"text","text":"Intro."}]},
				{"type":"paragraph","content":[{"type":"text","text":"Body."}]}
			]}`,
			want: "Intro.\n\nBody.",
		},
		{
			name: "null description",
			raw:  "null",
			want: "",
		},
		{
			name:    "malformed JSON",
			raw:     `{"type":`,
			wantErr: true,
		},
		{
			name:    "wiki markup refused",
			raw:     `"h2. wiki markup"`,
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := descriptionText(json.RawMessage(tc.raw))
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}

	t.Run("nil description", func(t *testing.T) {
		t.Parallel()
		got, err := descriptionText(nil)
		require.NoError(t, err)
		require.Empty(t, got)
	})
}

// FuzzDescriptionText proves the walker never panics on arbitrary input, and that everything it accepts is an ADF
// document (or a blank/null description).
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
