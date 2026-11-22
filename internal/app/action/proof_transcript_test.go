package action_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/action"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/proof"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// The bounded transcript of a proof run (AM14e): a failed proof explains itself, and the file stays small enough for
// the gate's artifact limits. It holds tool names and sizes, never tool arguments or tool results.

func decodeTranscript(t *testing.T, raw []byte) action.ProofTranscript {
	t.Helper()
	require.NotEmpty(t, raw)
	var tr action.ProofTranscript
	require.NoError(t, json.Unmarshal(raw, &tr))
	return tr
}

func TestTheTranscriptNamesEachTurnWithoutToolResults(t *testing.T) {
	h := newProofHarness(t, nil)
	turns := []ports.AgentTurn{
		{Text: "reading the change", Calls: []ports.AgentCallBrief{{Name: "read_file", ArgumentBytes: 31}},
			ToolCalls: 1, Finish: ports.ModelFinishToolCalls, RequestBytes: 9100, CostUSD: 0.001,
			Usage: ports.ModelUsage{InputTokens: 2000, OutputTokens: 90}},
		{Text: strings.Repeat("x", 5000), Finish: ports.ModelFinishStop, Nudged: true, RequestBytes: 51000,
			TrimmedResults: 4, StallNudged: true, ReasoningBytes: 7000},
	}
	res, err := h.runWith(t, &fakeLoop{result: ports.AgentResult{Stop: ports.AgentStopNoProposal, Turns: turns,
		Transcript: []ports.ModelMessage{{Role: ports.ModelRoleTool, Content: "SECRET-TOOL-RESULT"}}}})
	require.NoError(t, err)

	require.NotContains(t, string(res.Transcript), "SECRET-TOOL-RESULT", "a tool result never enters the transcript")
	tr := decodeTranscript(t, res.Transcript)
	require.Equal(t, action.TranscriptVersion, tr.Version)
	require.Equal(t, ports.AgentStopNoProposal, tr.Stop)
	require.Equal(t, proof.KindNotReproduced, tr.Kind)
	require.Equal(t, proof.ReasonNoProposal, tr.Note)
	require.Equal(t, 2, tr.TurnsTotal)
	require.Len(t, tr.Turns, 2)

	first := tr.Turns[0]
	require.Equal(t, 1, first.N)
	require.Equal(t, []action.TranscriptCall{{Name: "read_file", ArgumentBytes: 31}}, first.Calls)
	require.Equal(t, 9100, first.RequestBytes)
	require.Equal(t, ports.ModelFinishToolCalls, first.Finish)
	require.Equal(t, "reading the change", first.Text)
	require.Equal(t, int64(2000), first.InputTokens)

	second := tr.Turns[1]
	require.Equal(t, 4, second.TrimmedResults)
	require.True(t, second.StallNudged)
	require.True(t, second.Nudged)
	require.Equal(t, 7000, second.ReasoningBytes)
	require.LessOrEqual(t, len(second.Text), action.MaxTranscriptText+len("..."), "the text is clipped to 2 KB")
	require.Equal(t, 5000, second.TextBytes)
}

// TestTheTranscriptStaysInsideItsBound fills every turn to its limit. The file keeps the newest turns whole, says how
// many it left out, and never passes MaxTranscriptBytes.
func TestTheTranscriptStaysInsideItsBound(t *testing.T) {
	h := newProofHarness(t, nil)
	var turns []ports.AgentTurn
	for i := range 40 {
		calls := make([]ports.AgentCallBrief, 16)
		for j := range calls {
			calls[j] = ports.AgentCallBrief{Name: strings.Repeat("n", 64), ArgumentBytes: 60_000}
		}
		turns = append(turns, ports.AgentTurn{Text: strings.Repeat(string(rune('a'+i%26)), 2048), Calls: calls,
			ToolCalls: 16, Finish: ports.ModelFinishToolCalls, RequestBytes: 48 << 10})
	}
	res, err := h.runWith(t, &fakeLoop{result: ports.AgentResult{Stop: ports.AgentStopTurns, Turns: turns},
		err: nil})
	require.NoError(t, err)

	require.LessOrEqual(t, len(res.Transcript), action.MaxTranscriptBytes)
	tr := decodeTranscript(t, res.Transcript)
	require.Equal(t, proof.ReasonTurnCap, tr.Reason)
	require.Equal(t, 40, tr.TurnsTotal)
	require.NotEmpty(t, tr.Turns)
	last := tr.Turns[len(tr.Turns)-1]
	require.Equal(t, 40, last.N, "the newest turn is kept: it explains the end")
	require.NotEmpty(t, last.Text)
	require.Equal(t, 40, tr.TurnsOmitted+len(tr.Turns))
	require.Positive(t, tr.TurnsOmitted+tr.TextsOmitted, "something was left out to fit")
}

// TestTheTranscriptOfARunThatNeverReachedTheModelIsEmpty: a trivial skip made no model call, so it has no transcript.
func TestTheTranscriptOfARunThatNeverReachedTheModelIsEmpty(t *testing.T) {
	h := newProofHarness(t, nil)
	h.in.Files = []ports.ChangedFile{{Path: "README.md", Additions: 1}}
	res, err := h.runWith(t, &fakeLoop{result: ports.AgentResult{Stop: ports.AgentStopDone}})
	require.NoError(t, err)
	require.Equal(t, proof.KindSkippedTrivial, res.Outcome.Kind)
	require.Nil(t, res.Transcript)
}
