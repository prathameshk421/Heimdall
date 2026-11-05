package deliberation_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/deliberation"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
)

func readGolden(t testing.TB) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/verdict.golden.json")
	require.NoError(t, err)
	return b
}

func goldenVerdict(t testing.TB) deliberation.Verdict {
	t.Helper()
	var v deliberation.Verdict
	require.NoError(t, json.Unmarshal(readGolden(t), &v))
	return v
}

func TestVerdictGoldenDecodesValidatesAndEncodesItBack(t *testing.T) {
	v := goldenVerdict(t)
	require.NoError(t, v.Validate())
	require.Equal(t, deliberation.DecisionUphold, v.Decision)
	require.Len(t, v.Citations, 4)
	require.Equal(t, deliberation.CitationFinding, v.Citations[3].Kind)
	require.Equal(t, finding.ConfidenceHigh, v.Confidence)
	out, err := json.Marshal(v)
	require.NoError(t, err)
	require.JSONEq(t, string(readGolden(t)), string(out), "golden JSON: the wire format is frozen")
}

func TestVerdictValidateRefusesWhatCannotBeSettled(t *testing.T) {
	long := strings.Repeat("q", 301)
	cases := []struct {
		name   string
		mutate func(*deliberation.Verdict)
		want   string
	}{
		{"case id that is not a case", func(v *deliberation.Verdict) { v.CaseID = "case_x" }, "case_id"},
		{"no decision", func(v *deliberation.Verdict) { v.Decision = "" }, "decision"},
		{"rationale too short", func(v *deliberation.Verdict) { v.Rationale = "no" }, "rationale must be 10 to 1000 characters, got 2"},
		{"rationale blank", func(v *deliberation.Verdict) { v.Rationale = strings.Repeat(" ", 30) }, "rationale must be 10 to 1000 characters, got 0"},
		{"rationale too long", func(v *deliberation.Verdict) { v.Rationale = strings.Repeat("r", 1001) }, "rationale must be 10 to 1000 characters, got 1001"},
		{"no citations", func(v *deliberation.Verdict) { v.Citations = nil }, "citations needs at least 1 entry"},
		{"too many citations", func(v *deliberation.Verdict) {
			v.Citations = make([]deliberation.Citation, 17)
			for i := range v.Citations {
				v.Citations[i] = v.Citations[0]
				v.Citations[i].Kind = deliberation.CitationRun
			}
		}, "citations has 17 entries, at most 16"},
		{"a citation kind outside the set", func(v *deliberation.Verdict) { v.Citations[0].Kind = "vibes" }, "citations[0]: kind"},
		{"a run citation that is not a run id", func(v *deliberation.Verdict) { v.Citations[0].Ref = "run_1" }, "citations[0].ref"},
		{"a finding citation that is not a finding name", func(v *deliberation.Verdict) { v.Citations[3].Ref = "F0" }, "citations[3].ref"},
		{"a memory citation with a blank ref", func(v *deliberation.Verdict) { v.Citations[1].Ref = " " }, "citations[1].ref"},
		{"a decision citation with a blank ref", func(v *deliberation.Verdict) { v.Citations[2].Ref = "" }, "citations[2].ref"},
		{"a memory ref over 256 bytes", func(v *deliberation.Verdict) { v.Citations[1].Ref = strings.Repeat("m", 257) }, "citations[1].ref"},
		{"a citation with no quote", func(v *deliberation.Verdict) { v.Citations[0].Quote = "" }, "citations[0].quote"},
		{"a quote over 300 characters", func(v *deliberation.Verdict) { v.Citations[0].Quote = long }, "citations[0].quote"},
		{"a confidence outside the set", func(v *deliberation.Verdict) { v.Confidence = "sure" }, "confidence"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v := goldenVerdict(t)
			c.mutate(&v)
			err := v.Validate()
			require.Error(t, err)
			require.Contains(t, err.Error(), c.want)
		})
	}
}

func TestVerdictValidateReportsEveryProblemAtOnce(t *testing.T) {
	v := goldenVerdict(t)
	v.CaseID = "nope"
	v.Rationale = "x"
	v.Citations[0].Quote = ""
	err := v.Validate()
	require.Error(t, err)
	for _, want := range []string{"case_id", "rationale", "citations[0].quote"} {
		require.Contains(t, err.Error(), want)
	}
}

func TestVerdictRefusesUnknownEnumsWhenDecoded(t *testing.T) {
	g := string(readGolden(t))
	for name, c := range map[string][2]string{
		"decision":   {`"decision": "uphold"`, `"decision": "maybe"`},
		"confidence": {`"confidence": "high"`, `"confidence": "sure"`},
		"kind":       {`"kind": "run"`, `"kind": "vibes"`},
	} {
		t.Run(name, func(t *testing.T) {
			require.Contains(t, g, c[0])
			var v deliberation.Verdict
			require.Error(t, json.Unmarshal([]byte(strings.Replace(g, c[0], c[1], 1)), &v))
		})
	}
}

func TestEveryDecisionParsesItsOwnValuesAndRefusesOthers(t *testing.T) {
	for _, s := range []string{"uphold", "dismiss", "amend", "spec_change", "needs_human"} {
		_, err := deliberation.ParseDecision(s)
		require.NoError(t, err, s)
	}
	for _, bad := range []string{"", "Uphold", "needs-human", "escalate", "uphold "} {
		_, err := deliberation.ParseDecision(bad)
		require.Error(t, err, bad)
	}
}

func FuzzDecodeVerdict(f *testing.F) {
	f.Add(readGolden(f))
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"case_id":"case_01HQZX3K8V0M5N7P2R4S6T8W9Y","decision":"dismiss","rationale":"0123456789","citations":[{"kind":"finding","ref":"F2","quote":"q"}],"confidence":"low"}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		var v deliberation.Verdict
		if json.Unmarshal(data, &v) != nil || v.Validate() != nil {
			return
		}
		out, err := json.Marshal(v)
		require.NoError(t, err)
		var again deliberation.Verdict
		require.NoError(t, json.Unmarshal(out, &again), "accepted input must re-decode: %s", out)
		require.NoError(t, again.Validate(), "accepted input must re-validate: %s", out)
		require.Equal(t, v, again)
	})
}

func TestVerdictLimitsAreInclusive(t *testing.T) {
	v := goldenVerdict(t)
	v.Rationale = strings.Repeat("r", 10)
	require.NoError(t, v.Validate(), "10 characters is the minimum rationale")
	v.Rationale = strings.Repeat("r", 1000)
	require.NoError(t, v.Validate(), "1000 characters is the maximum rationale")

	v = goldenVerdict(t)
	v.Citations = nil
	for range deliberation.MaxCitations {
		v.Citations = append(v.Citations, deliberation.Citation{Kind: deliberation.CitationMemory, Ref: "mem_x", Quote: "q"})
	}
	require.NoError(t, v.Validate(), "16 citations is the maximum")

	v = goldenVerdict(t)
	v.Citations[1].Ref = strings.Repeat("m", 256)
	require.NoError(t, v.Validate(), "a 256-byte memory ref is the maximum")

	v = goldenVerdict(t)
	v.Citations[0].Quote = strings.Repeat("q", deliberation.MaxQuoteChars)
	require.NoError(t, v.Validate(), "300 characters is the maximum quote")
}
