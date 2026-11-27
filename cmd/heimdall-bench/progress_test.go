package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/bench"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

var errTestRun = errors.New("no result")

func TestProgressNamesTheToolsOfAnAnswer(t *testing.T) {
	// An answer with no tool call is a stop the operator must see, so the field is "-" and never empty.
	require.Equal(t, "-", toolNames(nil))
	require.Equal(t, "list_changes", toolNames([]string{"list_changes"}))
	require.Equal(t, "a,b", toolNames([]string{"a", "b"}))
}

func TestProgressReportsAFailedTestRun(t *testing.T) {
	// A run that produced no result at all must say so. A zero count would read as a fast pass.
	var b strings.Builder
	cp := newCaseProgress(newProgress(&b), "case")
	cp.testEnd("abc", ports.SandboxResult{}, errTestRun, 1.5)
	require.Contains(t, b.String(), "done error=yes 1.5s")
}

func TestVerdictOfACase(t *testing.T) {
	// One word says what the case found. A bug the reviewer located but could not prove is not a hit, and a clean case
	// that stayed quiet is not a false positive.
	tests := []struct {
		rc   reviewCase
		want string
	}{
		{reviewCase{CaseScore: bench.CaseScore{Kind: bench.KindBug, Scored: true, Hit: true}}, "hit"},
		{reviewCase{CaseScore: bench.CaseScore{Kind: bench.KindBug, Scored: true, Located: true}}, "located"},
		{reviewCase{CaseScore: bench.CaseScore{Kind: bench.KindBug, Scored: true}}, "miss"},
		{reviewCase{CaseScore: bench.CaseScore{Kind: bench.KindClean, Scored: true, FalsePositive: true}}, "false_positive"},
		{reviewCase{CaseScore: bench.CaseScore{Kind: bench.KindClean, Scored: true}}, "clean"},
		{reviewCase{CaseScore: bench.CaseScore{Kind: bench.KindBug}, Error: "model error"}, "error"},
		{reviewCase{CaseScore: bench.CaseScore{Kind: bench.KindBug}}, "unscored"},
	}
	for _, tt := range tests {
		require.Equal(t, tt.want, verdictOf(tt.rc))
	}
}
