package finding_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
)

// withLocationFile validates the golden finding with its location file replaced, and reports whether it was accepted.
func withLocationFile(t *testing.T, file string) bool {
	t.Helper()
	f := goldenFinding(t)
	f.Location.File = file
	return f.Validate() == nil
}

func TestLocationFileRuleAcceptsOnlyCleanRelativePaths(t *testing.T) {
	cases := []struct {
		file string
		ok   bool
	}{
		{"main.go", true},
		{"internal/refunds/refunds.go", true},
		{"...", true},
		{strings.Repeat("a", 512), true},
		{strings.Repeat("a", 513), false},
		{"", false},
		{".", false},
		{"..", false},
		{"../x.go", false},
		{"a/../b.go", false},
		{"a/./b.go", false},
		{"a//b.go", false},
		{"a/", false},
		{"/abs.go", false},
		{`a\b.go`, false},
		{"a\x00b.go", false},
		{"a\nb.go", false},
	}
	for _, c := range cases {
		require.Equal(t, c.ok, withLocationFile(t, c.file), "location.file %q", c.file)
	}
}

// withClaim validates the golden finding with its claim replaced, and reports whether it was accepted.
func withClaim(t *testing.T, claim string) bool {
	t.Helper()
	f := goldenFinding(t)
	f.Claim = claim
	return f.Validate() == nil
}

func TestClaimLengthIsCountedInCharactersAfterTrimming(t *testing.T) {
	require.False(t, withClaim(t, "   "+strings.Repeat("x", 9)+"   "), "9 characters after trimming is too short")
	require.True(t, withClaim(t, "   "+strings.Repeat("x", 10)+"   "), "10 characters after trimming is the minimum")
	require.True(t, withClaim(t, strings.Repeat("界", 600)), "600 runes is the maximum even though each takes 3 bytes")
	require.False(t, withClaim(t, strings.Repeat("界", 601)), "601 runes is over the maximum")
}

// withReasoning validates the golden finding with its only reasoning evidence replaced, and reports acceptance.
func withReasoning(t *testing.T, text string) bool {
	t.Helper()
	f := goldenFinding(t)
	f.Evidence = []finding.Evidence{finding.NewReasoningEvidence(text)}
	return f.Validate() == nil
}

func TestReasoningHasTheSameCharacterRulesAsAClaim(t *testing.T) {
	require.False(t, withReasoning(t, "  nine char  "), "9 characters after trimming is too short")
	require.True(t, withReasoning(t, "ten chars!"), "10 characters is the minimum")
	require.True(t, withReasoning(t, strings.Repeat("é", 600)))
	require.False(t, withReasoning(t, strings.Repeat("é", 601)), "601 characters is over the maximum")
}

// withMemoryRef validates the golden finding with a single memory evidence whose ref is replaced.
func withMemoryRef(t *testing.T, ref string) bool {
	t.Helper()
	f := goldenFinding(t)
	f.Evidence = []finding.Evidence{finding.NewMemoryEvidence(ref)}
	return f.Validate() == nil
}

func TestRefsAreBoundedInBytes(t *testing.T) {
	require.True(t, withMemoryRef(t, strings.Repeat("m", 256)))
	require.False(t, withMemoryRef(t, strings.Repeat("m", 257)))
	require.False(t, withMemoryRef(t, "   "), "a blank ref is not a ref")
}

func TestUnknownEvidenceTypeIsRefusedWhenDecodedButMissingTypeIsReportedByValidate(t *testing.T) {
	var e finding.Evidence
	require.Error(t, json.Unmarshal([]byte(`{"type":"hunch","reasoning":"a guess with no basis"}`), &e))
	var missing finding.Evidence
	require.NoError(t, json.Unmarshal([]byte(`{"reasoning":"a reason with no type given"}`), &missing))
	require.Error(t, missing.Validate(), "a payload with no type must be reported by Validate, not silently accepted")
}

func TestEightEvidenceAndEightContextRefsAreAccepted(t *testing.T) {
	f := goldenFinding(t)
	f.Evidence = nil
	for range finding.MaxEvidence {
		f.Evidence = append(f.Evidence, finding.NewReasoningEvidence("the ledger still reads the field"))
	}
	f.ContextRefs = nil
	for range finding.MaxContextRefs {
		f.ContextRefs = append(f.ContextRefs, "mem_ledger_reads_refund_ref")
	}
	require.NoError(t, f.Validate(), "the limits are inclusive")
}

func TestASingleLineLocationIsValid(t *testing.T) {
	f := goldenFinding(t)
	f.Location.Lines = [2]int{7, 7}
	require.NoError(t, f.Validate(), "the first line may equal the last")
}

func TestBlankRequiredFieldsOfPayloadsAreRefused(t *testing.T) {
	cases := map[string]func(*finding.Input){
		"repro test name of spaces":    func(f *finding.Input) { f.Evidence[0].ReproTest.Name = "   " },
		"repro test content of spaces": func(f *finding.Input) { f.Evidence[0].ReproTest.Content = " \n\t " },
		"tool name of spaces":          func(f *finding.Input) { f.Evidence[1].ToolResult.Tool = "  " },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			f := goldenFinding(t)
			mutate(&f)
			require.Error(t, f.Validate())
		})
	}
}

func TestAnUnknownEvidenceTypeIsNamedAsUnknown(t *testing.T) {
	e := finding.Evidence{Type: "hunch", Reasoning: new("a guess with no basis")}
	require.ErrorContains(t, e.Validate(), `evidence: type "hunch" is not a known evidence type`)
}
