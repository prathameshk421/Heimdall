package finding_test

import (
	"encoding/json"
	"math/rand/v2"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
)

func read(t testing.TB, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	require.NoError(t, err)
	return b
}

func goldenFinding(t testing.TB) finding.Input {
	t.Helper()
	var f finding.Input
	require.NoError(t, json.Unmarshal(read(t, "finding_in.golden.json"), &f))
	return f
}

func TestFindingInDecodesTheGoldenValidatesAndEncodesItBack(t *testing.T) {
	f := goldenFinding(t)
	require.NoError(t, f.Validate())
	require.Equal(t, finding.CategoryCompat, f.Category)
	require.Equal(t, [2]int{40, 52}, f.Location.Lines)
	require.Len(t, f.Evidence, 5)
	out, err := json.Marshal(f)
	require.NoError(t, err)
	require.JSONEq(t, string(read(t, "finding_in.golden.json")), string(out), "golden JSON: the wire format is frozen")
}

func str(s string) *string { return &s }

func TestFindingInValidateRefusesWhatAnAgentMustFix(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*finding.Input)
		want   string
	}{
		{"claim too short", func(f *finding.Input) { f.Claim = "bad" }, "claim must be 10 to 600 characters, got 3"},
		{"claim blank", func(f *finding.Input) { f.Claim = strings.Repeat(" ", 20) }, "claim must be 10 to 600 characters, got 0"},
		{"claim too long", func(f *finding.Input) { f.Claim = strings.Repeat("x", 601) }, "claim must be 10 to 600 characters, got 601"},
		{"claim counts characters not bytes", func(f *finding.Input) { f.Claim = strings.Repeat("é", 600) }, ""},
		{"nine context refs", func(f *finding.Input) { f.ContextRefs = strings.Split("a,b,c,d,e,f,g,h,i", ",") }, "context_refs has 9 entries, at most 8"},
		{"an empty context ref", func(f *finding.Input) { f.ContextRefs = []string{""} }, "context_refs[0]"},
		{"no evidence", func(f *finding.Input) { f.Evidence = nil }, "evidence needs at least 1 entry"},
		{"nine evidence entries", func(f *finding.Input) {
			f.Evidence = make([]finding.Evidence, 9)
			for i := range f.Evidence {
				f.Evidence[i] = finding.NewReasoningEvidence("because the code says so")
			}
		}, "evidence has 9 entries, at most 8"},
		{"two evidence payloads set", func(f *finding.Input) { f.Evidence[2].PackRef = str("pack:x") }, "evidence[2]: exactly one payload"},
		{"evidence type and payload disagree", func(f *finding.Input) { f.Evidence[2].Type = finding.EvidencePackRef }, "evidence[2]: type is pack_ref but pack_ref is not set"},
		{"evidence with no payload", func(f *finding.Input) { f.Evidence[4] = finding.Evidence{Type: finding.EvidenceReasoning} }, "evidence[4]"},
		{"evidence with no type", func(f *finding.Input) { f.Evidence[3].Type = "" }, "evidence[3]: type"},
		{"line range backwards", func(f *finding.Input) { f.Location.Lines = [2]int{52, 40} }, "location.lines"},
		{"line zero", func(f *finding.Input) { f.Location.Lines = [2]int{0, 5} }, "location.lines"},
		{"empty file", func(f *finding.Input) { f.Location.File = "" }, "location.file"},
		{"file escapes the repo", func(f *finding.Input) { f.Location.File = "../../etc/passwd" }, "location.file"},
		{"absolute file", func(f *finding.Input) { f.Location.File = "/etc/passwd" }, "location.file"},
		{"unclean file", func(f *finding.Input) { f.Location.File = "a/./b.go" }, "location.file"},
		{"zero value category", func(f *finding.Input) { f.Category = "" }, "category"},
		{"zero value severity", func(f *finding.Input) { f.Severity = "urgent" }, "severity"},
		{"zero value confidence", func(f *finding.Input) { f.Confidence = "" }, "confidence"},
		{"repro test without a name", func(f *finding.Input) { f.Evidence[0].ReproTest.Name = "" }, "evidence[0].repro_test.name"},
		{"repro test without a path", func(f *finding.Input) { f.Evidence[0].ReproTest.Path = "" }, "evidence[0].repro_test.path"},
		{"repro test path escapes", func(f *finding.Input) { f.Evidence[0].ReproTest.Path = "../x_test.go" }, "evidence[0].repro_test.path"},
		{"repro test without content", func(f *finding.Input) { f.Evidence[0].ReproTest.Content = "" }, "evidence[0].repro_test.content"},
		{"tool result without a tool", func(f *finding.Input) { f.Evidence[1].ToolResult.Tool = "" }, "evidence[1].tool_result.tool"},
		{"tool result with a bad location", func(f *finding.Input) { f.Evidence[1].ToolResult.Location.Lines = [2]int{3, 1} }, "evidence[1].tool_result.location.lines"},
		{"empty memory ref", func(f *finding.Input) { f.Evidence[2].MemoryRef = str("") }, "evidence[2].memory_ref"},
		{"empty pack ref", func(f *finding.Input) { f.Evidence[3].PackRef = str(" ") }, "evidence[3].pack_ref"},
		{"reasoning too short", func(f *finding.Input) { f.Evidence[4].Reasoning = str("meh") }, "evidence[4].reasoning"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := goldenFinding(t)
			c.mutate(&f)
			err := f.Validate()
			if c.want == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			require.Contains(t, err.Error(), c.want)
		})
	}
}

func TestFindingInValidateReportsEveryProblemAtOnce(t *testing.T) {
	f := goldenFinding(t)
	f.Claim = "bad"
	f.Location.Lines = [2]int{0, 0}
	f.Evidence = nil
	err := f.Validate()
	require.Error(t, err)
	for _, want := range []string{"claim", "location.lines", "evidence"} {
		require.Contains(t, err.Error(), want)
	}
}

func TestFindingInRefusesUnknownEnumValuesWhenDecoded(t *testing.T) {
	g := string(read(t, "finding_in.golden.json"))
	for name, c := range map[string][2]string{
		"category":      {`"category": "compat"`, `"category": "vibes"`},
		"severity":      {`"severity": "high"`, `"severity": "scary"`},
		"confidence":    {`"confidence": "high"`, `"confidence": "sure"`},
		"evidence type": {`"type": "memory"`, `"type": "hunch"`},
	} {
		t.Run(name, func(t *testing.T) {
			require.Contains(t, g, c[0])
			var f finding.Input
			err := json.Unmarshal([]byte(strings.Replace(g, c[0], c[1], 1)), &f)
			require.Error(t, err)
			require.Contains(t, err.Error(), name[:4])
		})
	}
}

func TestFindingInRefusesUnknownFieldsWhenDecodedStrictly(t *testing.T) {
	var f finding.Input
	dec := json.NewDecoder(strings.NewReader(`{"claim":"x","surprise":1}`))
	dec.DisallowUnknownFields()
	require.Error(t, dec.Decode(&f))
}

func randText(r *rand.Rand, minLen, maxLen int) string {
	const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789 _-.:é\"\\\n"
	runes := []rune(alphabet)
	n := minLen + r.IntN(maxLen-minLen+1)
	var b strings.Builder
	for len([]rune(b.String())) < n {
		b.WriteRune(runes[r.IntN(len(runes))])
	}
	return b.String()
}

func randToken(r *rand.Rand) string {
	const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789_-"
	n := 3 + r.IntN(20)
	b := make([]byte, n)
	for i := range b {
		b[i] = alphabet[r.IntN(len(alphabet))]
	}
	return string(b)
}

func randEvidence(r *rand.Rand) finding.Evidence {
	switch r.IntN(5) {
	case 0:
		return finding.NewReproTestEvidence(finding.ReproTest{Path: "tests/" + randToken(r) + "_test.go", Name: "Test" + randToken(r), Content: randText(r, 1, 300)})
	case 1:
		args := make([]string, r.IntN(4))
		for i := range args {
			args[i] = randToken(r)
		}
		return finding.NewToolResultEvidence(finding.ToolResult{Tool: randToken(r), Args: args, Location: finding.Location{File: randToken(r) + ".go", Lines: [2]int{1 + r.IntN(50), 51 + r.IntN(50)}}})
	case 2:
		return finding.NewMemoryEvidence("mem_" + randToken(r))
	case 3:
		return finding.NewPackRefEvidence("pack:" + randToken(r))
	}
	return finding.NewReasoningEvidence(randText(r, 10, 400))
}

func TestEveryValidEvidenceRoundTripsThroughJSONToAnEqualValue(t *testing.T) {
	r := rand.New(rand.NewPCG(7, 11))
	seen := map[finding.EvidenceType]bool{}
	for range 2000 {
		e := randEvidence(r)
		require.NoError(t, e.Validate(), "%+v", e)
		seen[e.Type] = true
		b, err := json.Marshal(e)
		require.NoError(t, err)
		var back finding.Evidence
		require.NoError(t, json.Unmarshal(b, &back))
		require.Equal(t, e, back)
		require.NoError(t, back.Validate())
	}
	require.Len(t, seen, 5, "the generator must reach every evidence type")
}

func TestEvidenceConstructorsSetExactlyTheirOwnPayload(t *testing.T) {
	for _, e := range []finding.Evidence{
		finding.NewReproTestEvidence(finding.ReproTest{Path: "a_test.go", Name: "TestA", Content: "x"}),
		finding.NewToolResultEvidence(finding.ToolResult{Tool: "go vet", Location: finding.Location{File: "a.go", Lines: [2]int{1, 1}}}),
		finding.NewMemoryEvidence("mem_x"),
		finding.NewPackRefEvidence("pack:x"),
		finding.NewReasoningEvidence("because of reasons"),
	} {
		require.NoError(t, e.Validate(), e.Type)
		b, err := json.Marshal(e)
		require.NoError(t, err)
		var m map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(b, &m))
		require.Len(t, m, 2, "type plus one payload: %s", b)
	}
}

func FuzzDecodeFindingIn(f *testing.F) {
	f.Add(read(f, "finding_in.golden.json"))
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"claim":"0123456789","evidence":[{"type":"memory","memory_ref":"m"}]}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		var in finding.Input
		if json.Unmarshal(data, &in) != nil || in.Validate() != nil {
			return
		}
		out, err := json.Marshal(in)
		require.NoError(t, err)
		var again finding.Input
		require.NoError(t, json.Unmarshal(out, &again), "accepted input must re-decode: %s", out)
		require.NoError(t, again.Validate(), "accepted input must re-validate: %s", out)
		require.Equal(t, in, again)
	})
}
