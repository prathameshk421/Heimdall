package commands

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// fixedPeople maps "<source>:<sender>" to a person name.
type fixedPeople map[string]string

func (p fixedPeople) Lookup(src Source, sender string) (string, bool) {
	name, ok := p[string(src)+":"+sender]
	return name, ok
}

var testPeople = fixedPeople{
	"chat:u-ana":    "ana",
	"github:ana-gh": "ana",
	"chat:u-bob":    "bob",
}

func chat(text string) Input { return Input{Text: text, Source: SourceChat, Sender: "u-ana"} }
func gh(text string) Input   { return Input{Text: text, Source: SourceGitHub, Sender: "ana-gh"} }

func TestParseCommands(t *testing.T) {
	long := "this reason is long enough"
	tests := []struct {
		name string
		in   Input
		want Command
	}{
		{"chat uphold", chat("heimdall uphold F3"), Uphold{Finding: "F3"}},
		{"github uphold", gh("/heimdall uphold F3"), Uphold{Finding: "F3"}},
		{"case-insensitive word", chat("HeImDaLl uphold f12"), Uphold{Finding: "F12"}},
		{"leading space and newline", chat("  \n heimdall uphold F1\n"), Uphold{Finding: "F1"}},
		{"tabs between words", chat("heimdall\tuphold\t\tF1"), Uphold{Finding: "F1"}},
		{"start", chat("heimdall start KAN-12"), Start{Ticket: "KAN-12"}},
		{"start lowercase key", chat("heimdall start kan-12"), Start{Ticket: "KAN-12"}},
		{"review", chat("heimdall review @a @b @c"), Review{Reviewers: []string{"a", "b", "c"}}},
		{"review one", gh("/heimdall review @octo-cat"), Review{Reviewers: []string{"octo-cat"}}},
		{"dismiss", chat("heimdall dismiss F2 " + long), Dismiss{Finding: "F2", Reason: long}},
		{"dismiss exactly 15", chat("heimdall dismiss F2 123456789012345"), Dismiss{Finding: "F2", Reason: "123456789012345"}},
		{"spec-change", chat("heimdall spec-change limit is 100 not 50"), SpecChange{Text: "limit is 100 not 50"}},
		{"override", chat("heimdall override case-9 " + long), Override{Case: "case-9", Reason: long}},
		{"waive-reviewer", chat("heimdall waive-reviewer @rev " + long), WaiveReviewer{Reviewer: "rev", Reason: long}},
		{"reject", chat("heimdall reject " + long), Reject{Reason: long}},
		{"approve-tests", chat("heimdall approve-tests"), ApproveTests{}},
		{"approve-profile", chat("heimdall approve-profile " + strings.Repeat("a", 64)), ApproveProfile{ID: strings.Repeat("a", 64)}},
		{"approve-knowledge", chat("heimdall approve-knowledge " + strings.Repeat("a", 64)), ApproveKnowledge{Ref: "knowledge:" + strings.Repeat("a", 64)}},
		{"promote-profile", gh("/heimdall promote-profile " + strings.Repeat("b", 64)), PromoteProfile{ID: strings.Repeat("b", 64)}},
		{"incident", gh("/heimdall incident double charge in prod"), Incident{Text: "double charge in prod"}},
		{"challenge", chat("heimdall challenge F3 internal/pay/pay_test.go:42"), Challenge{Finding: "F3", Evidence: "internal/pay/pay_test.go:42"}},
		{"challenge multiword evidence", chat("heimdall challenge F3 run 01JABC passes at head"), Challenge{Finding: "F3", Evidence: "run 01JABC passes at head"}},
		{"remember", chat("heimdall remember refunds are capped at 500"), Remember{Text: "refunds are capped at 500"}},
		{"remember multiline", chat("heimdall remember line one\nline two"), Remember{Text: "line one\nline two"}},
		{"forget", chat("heimdall forget mem_abc " + long), Forget{MemoryRef: "mem_abc", Reason: long}},
		{"forget knowledge source", chat("heimdall forget knowledge:" + strings.Repeat("a", 64) + " " + long), Forget{MemoryRef: "knowledge:" + strings.Repeat("a", 64), Reason: long}},
		{"github mapped by login", gh("/heimdall approve-tests"), ApproveTests{}},
		{"other mapped person", Input{Text: "heimdall approve-tests", Source: SourceChat, Sender: "u-bob"}, ApproveTests{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Parse(tc.in, testPeople)
			require.Equal(t, KindCommand, got.Kind, "reply: %s", got.Reply)
			require.Equal(t, tc.want, got.Command)
			require.Empty(t, got.Reply)
			require.NotEmpty(t, got.Person)
		})
	}
}

func TestParseVerbOnResult(t *testing.T) {
	got := Parse(chat("heimdall uphold F3"), testPeople)
	require.Equal(t, "uphold", got.Verb)
	require.Equal(t, "uphold", got.Command.Verb())
	require.Equal(t, "ana", got.Person)
}

func TestParseNotCommand(t *testing.T) {
	tests := []struct {
		name string
		in   Input
	}{
		{"empty", chat("")},
		{"blank", chat("  \n\t ")},
		{"plain talk", chat("I think we should uphold F3")},
		{"word mid-sentence", chat("please heimdall uphold F3")},
		{"bang form is never delivered", chat("!heimdall uphold F3")},
		{"slash form in chat", chat("/heimdall uphold F3")},
		{"mention form", chat("@heimdall uphold F3")},
		{"longer word", chat("heimdallx uphold F3")},
		{"glued", chat("heimdall-uphold F3")},
		{"plain word on github", gh("heimdall uphold F3")},
		{"bang on github", gh("!heimdall uphold F3")},
		{"slash longer word on github", gh("/heimdalls uphold F3")},
		{"quoted reply", chat("> heimdall uphold F3")},
		{"code fence", chat("```\nheimdall uphold F3\n```")},
		{"unknown source", Input{Text: "heimdall uphold F3", Source: Source("sms"), Sender: "u-ana"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Parse(tc.in, testPeople)
			require.Equal(t, KindNotCommand, got.Kind)
			require.Nil(t, got.Command)
			require.Empty(t, got.Reply)
		})
	}
}

func TestParseUnmappedSender(t *testing.T) {
	tests := []struct {
		name string
		in   Input
	}{
		{"agent sender", Input{Text: "heimdall uphold F3", Source: SourceChat, Sender: "agent-reviewer-1"}},
		{"empty sender", Input{Text: "heimdall uphold F3", Source: SourceChat}},
		{"chat id used on github", Input{Text: "/heimdall uphold F3", Source: SourceGitHub, Sender: "u-ana"}},
		{"github login used in chat", Input{Text: "heimdall uphold F3", Source: SourceChat, Sender: "ana-gh"}},
		{"case differs (matched whole)", Input{Text: "heimdall uphold F3", Source: SourceChat, Sender: "U-ANA"}},
		{"padded sender", Input{Text: "heimdall uphold F3", Source: SourceChat, Sender: " u-ana"}},
		{"unmapped sender with bad syntax gets no grammar", Input{Text: "heimdall frobnicate", Source: SourceChat, Sender: "agent-1"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Parse(tc.in, testPeople)
			require.Equal(t, KindUnmapped, got.Kind)
			require.Nil(t, got.Command)
			require.Empty(t, got.Person)
			require.NotEmpty(t, got.Reply, "caller needs the link hint for a human who is unmapped")
			require.NotContains(t, got.Reply, "uphold <finding>", "an unmapped sender must not receive the grammar")
		})
	}
}

func TestParseNilPeopleRefusesEveryone(t *testing.T) {
	got := Parse(chat("heimdall uphold F3"), nil)
	require.Equal(t, KindUnmapped, got.Kind)
}

func TestParseUnknownCommand(t *testing.T) {
	tests := []struct {
		name string
		in   Input
		verb string
	}{
		{"unknown verb", chat("heimdall frobnicate F3"), "frobnicate"},
		{"bare word", chat("heimdall"), ""},
		{"bare word with space", chat("heimdall   "), ""},
		{"github bare", gh("/heimdall"), ""},
		{"verb case folded", chat("heimdall UPHOLDX F3"), "upholdx"},
		{"approve tests with space", chat("heimdall approve tests"), "approve"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Parse(tc.in, testPeople)
			require.Equal(t, KindUnknown, got.Kind)
			require.Nil(t, got.Command)
			require.Equal(t, tc.verb, got.Verb)
			require.Equal(t, "ana", got.Person)
			require.Contains(t, got.Reply, "heimdall uphold <finding>")
			require.Contains(t, got.Reply, "heimdall remember <text>")
		})
	}
}

func TestUnknownReplyUsesSourcePrefix(t *testing.T) {
	require.Contains(t, Parse(chat("heimdall nope"), testPeople).Reply, "heimdall start <ticket>")
	require.NotContains(t, Parse(chat("heimdall nope"), testPeople).Reply, "/heimdall")
	require.Contains(t, Parse(gh("/heimdall nope"), testPeople).Reply, "/heimdall start <ticket>")
}

func TestParseBadArgs(t *testing.T) {
	short := "too short"
	tests := []struct {
		name    string
		in      Input
		verb    string
		contain string
	}{
		{"uphold no arg", chat("heimdall uphold"), "uphold", "heimdall uphold <finding>"},
		{"uphold extra arg", chat("heimdall uphold F3 F4"), "uphold", "one finding"},
		{"uphold ulid refused", chat("heimdall uphold 01ARZ3NDEKTSV4RRFFQ69G5FAV"), "uphold", "F3"},
		{"uphold zero", chat("heimdall uphold F0"), "uphold", "F3"},
		{"uphold leading zero", chat("heimdall uphold F03"), "uphold", "F3"},
		{"uphold no digits", chat("heimdall uphold F"), "uphold", "F3"},
		{"uphold bare number", chat("heimdall uphold 3"), "uphold", "F3"},
		{"uphold absurdly long number", chat("heimdall uphold F" + strings.Repeat("9", 40)), "uphold", "F3"},
		{"dismiss short reason", chat("heimdall dismiss F2 " + short), "dismiss", "15"},
		{"dismiss 14 runes", chat("heimdall dismiss F2 12345678901234"), "dismiss", "15"},
		{"dismiss no reason", chat("heimdall dismiss F2"), "dismiss", "15"},
		{"dismiss no args", chat("heimdall dismiss"), "dismiss", "heimdall dismiss <finding>"},
		{"start no key", chat("heimdall start"), "start", "heimdall start <ticket>"},
		{"start bad key", chat("heimdall start 12"), "start", "KAN-12"},
		{"start two keys", chat("heimdall start KAN-1 KAN-2"), "start", "one ticket"},
		{"start key injection", chat("heimdall start KAN-1;rm"), "start", "KAN-12"},
		{"review no one", chat("heimdall review"), "review", "heimdall review @a"},
		{"review without at", chat("heimdall review ana"), "review", "@"},
		{"review duplicate", chat("heimdall review @a @a"), "review", "twice"},
		{"review four", chat("heimdall review @a @b @c @d"), "review", "1 to 3"},
		{"review bad handle", chat("heimdall review @a/b"), "review", "handle"},
		{"approve-tests with arg", chat("heimdall approve-tests now"), "approve-tests", "no arguments"},
		{"challenge no evidence", chat("heimdall challenge F3"), "challenge", "evidence"},
		{"challenge no args", chat("heimdall challenge"), "challenge", "heimdall challenge <finding>"},
		{"challenge bad finding", chat("heimdall challenge x some evidence"), "challenge", "F3"},
		{"remember empty", chat("heimdall remember"), "remember", "heimdall remember <text>"},
		{"remember blanks", chat("heimdall remember   \n "), "remember", "heimdall remember <text>"},
		{"incident empty", chat("heimdall incident"), "incident", "heimdall incident <text>"},
		{"spec-change empty", chat("heimdall spec-change"), "spec-change", "heimdall spec-change <text>"},
		{"reject empty", chat("heimdall reject"), "reject", "heimdall reject <reason>"},
		{"override no reason", chat("heimdall override case-9"), "override", "heimdall override <case>"},
		{"override bad case", chat("heimdall override a/b some reason here"), "override", "case"},
		{"waive no reason", chat("heimdall waive-reviewer @x"), "waive-reviewer", "heimdall waive-reviewer @x"},
		{"waive no at", chat("heimdall waive-reviewer x because reasons"), "waive-reviewer", "@"},
		{"forget no reason", chat("heimdall forget mem_abc"), "forget", "heimdall forget <memory_ref>"},
		{"forget bad ref", chat("heimdall forget a/b some reason here"), "forget", "memory_ref"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Parse(tc.in, testPeople)
			require.Equal(t, KindBadArgs, got.Kind, "reply: %s", got.Reply)
			require.Nil(t, got.Command)
			require.Equal(t, tc.verb, got.Verb)
			require.Equal(t, "ana", got.Person)
			require.Contains(t, got.Reply, tc.contain)
		})
	}
}

func TestDismissCountsRunesNotBytes(t *testing.T) {
	fifteen := strings.Repeat("é", 15)
	require.Equal(t, KindCommand, Parse(chat("heimdall dismiss F2 "+fifteen), testPeople).Kind)
	require.Equal(t, KindBadArgs, Parse(chat("heimdall dismiss F2 "+strings.Repeat("é", 14)), testPeople).Kind)
}

func TestFindingNeverUlid(t *testing.T) {
	got := Parse(chat("heimdall challenge 01ARZ3NDEKTSV4RRFFQ69G5FAV some evidence"), testPeople)
	require.Equal(t, KindBadArgs, got.Kind)
	require.Contains(t, got.Reply, "F3")
}

func TestReplyIsSingleShortMessage(t *testing.T) {
	got := Parse(chat("heimdall nope"), testPeople)
	require.Less(t, len(got.Reply), 1500)
}
