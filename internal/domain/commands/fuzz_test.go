package commands

import (
	"testing"
)

type fuzzPeople struct{}

func (fuzzPeople) Lookup(src Source, sender string) (string, bool) {
	if sender == "known" && (src == SourceChat || src == SourceGitHub) {
		return "person", true
	}
	return "", false
}

func FuzzParse(f *testing.F) {
	for _, seed := range []struct {
		source Source
		text   string
		sender string
	}{
		{SourceChat, "heimdall start KAN-1", "known"},
		{SourceGitHub, "/heimdall challenge F1 test passed", "known"},
		{SourceChat, "!heimdall remember fact", "known"},
		{SourceChat, "heimdall reject reason", "agent"},
		{Source("other"), "heimdall start KAN-1", "known"},
	} {
		f.Add(string(seed.source), seed.text, seed.sender)
	}
	f.Fuzz(func(t *testing.T, rawSource, text, sender string) {
		got := Parse(Input{Source: Source(rawSource), Text: text, Sender: sender}, fuzzPeople{})
		assertParseInvariant(t, got)
	})
}

func assertParseInvariant(t *testing.T, got Result) {
	t.Helper()
	if got.Kind < KindNotCommand || got.Kind > KindUnmapped {
		t.Fatalf("Parse returned invalid kind %d", got.Kind)
	}
	if got.Kind == KindCommand {
		if got.Command == nil || got.Person == "" || got.Verb != got.Command.Verb() {
			t.Fatalf("incomplete command result: %#v", got)
		}
		return
	}
	if got.Command != nil {
		t.Fatalf("non-command result carries executable command: %#v", got)
	}
	if got.Kind == KindNotCommand && got.Reply != "" {
		t.Fatalf("non-command text produced a reply: %q", got.Reply)
	}
	if got.Kind == KindUnmapped && got.Reply == "" {
		t.Fatal("unmapped command did not produce a refusal")
	}
}
