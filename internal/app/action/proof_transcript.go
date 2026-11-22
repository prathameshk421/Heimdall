package action

import (
	"encoding/json"
	"fmt"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/proof"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// The transcript of a proof run is a second file in the uploaded proof artifact, next to the result the gate reads. It
// lets a person see why a run ended: each turn's tool names and sizes, the request size, the trimmed results, the
// nudges, the finish reason and the first 2 KB of text. It never holds tool arguments, tool results or the key, and the
// gate never reads it, so it cannot change a check.

// The transcript format and its bounds.
const (
	// TranscriptVersion is the version this package writes.
	TranscriptVersion = 1
	// MaxTranscriptBytes bounds the file. With MaxArtifactBytes it fits the gate's download limits for the proof
	// artifact (cmd/heimdall proofLimits: 48 KB a file, 96 KB in all).
	MaxTranscriptBytes = 32 << 10
	// MaxTranscriptText bounds the text of one turn, as the loop's own record does.
	MaxTranscriptText = 2 << 10
	// maxTranscriptDetail bounds the stop detail: the words of a failed model call.
	maxTranscriptDetail = 300
)

// ProofTranscript is the bounded record of one proof run's turns.
type ProofTranscript struct {
	Version int             `json:"version"`
	Stop    ports.AgentStop `json:"stop"`
	Kind    proof.Kind      `json:"kind"`
	Reason  proof.Reason    `json:"reason,omitempty"`
	Note    proof.Reason    `json:"note,omitempty"`
	// Detail is the clipped error of a run that failed.
	Detail string `json:"detail,omitempty"`
	// TurnsTotal is the number of turns the run took. TurnsOmitted counts the oldest turns left out to fit the bound,
	// and TextsOmitted the turns kept without their text.
	TurnsTotal   int              `json:"turns_total"`
	TurnsOmitted int              `json:"turns_omitted,omitempty"`
	TextsOmitted int              `json:"texts_omitted,omitempty"`
	Turns        []TranscriptTurn `json:"turns"`
}

// TranscriptTurn is one model answer and the tool calls it asked for.
type TranscriptTurn struct {
	N              int               `json:"n"`
	RequestBytes   int               `json:"request_bytes"`
	TrimmedResults int               `json:"trimmed_results,omitempty"`
	Calls          []TranscriptCall  `json:"calls,omitempty"`
	ToolCalls      int               `json:"tool_calls"`
	Finish         ports.ModelFinish `json:"finish,omitempty"`
	Nudged         bool              `json:"nudged,omitempty"`
	StallNudged    bool              `json:"stall_nudged,omitempty"`
	Attempts       int               `json:"attempts,omitempty"`
	CostUSD        float64           `json:"cost_usd"`
	InputTokens    int64             `json:"input_tokens"`
	OutputTokens   int64             `json:"output_tokens"`
	ReasoningBytes int               `json:"reasoning_bytes,omitempty"`
	// TextBytes is the size of the text the loop recorded, and Text its first MaxTranscriptText bytes.
	TextBytes int    `json:"text_bytes,omitempty"`
	Text      string `json:"text,omitempty"`
}

// TranscriptCall is one tool call: its name, the size of its arguments, and the product's own error when it failed.
// It never holds the arguments or the result.
type TranscriptCall struct {
	Name          string `json:"name"`
	ArgumentBytes int    `json:"argument_bytes"`
	// Failed is set when the tool returned an error, and Error holds its first bytes: it is how a run that ended with
	// no result explains itself. The text is this product's own message, never anything the pull request wrote.
	Failed bool   `json:"failed,omitempty"`
	Error  string `json:"error,omitempty"`
}

// transcriptOf writes the bounded transcript of a run whose review reached the loop. To fit MaxTranscriptBytes it
// drops the text of the oldest turns first, then the oldest turns, so the turns that explain the end stay whole.
func transcriptOf(r *proofRun) ([]byte, error) {
	t := ProofTranscript{Version: TranscriptVersion, Stop: r.res.Stop, Kind: r.res.Outcome.Kind,
		Reason: r.res.Outcome.Reason, Note: r.res.Outcome.Note, Detail: clip(r.res.StopDetail, maxTranscriptDetail),
		TurnsTotal: len(r.turns), Turns: make([]TranscriptTurn, 0, len(r.turns))}
	for i, at := range r.turns {
		t.Turns = append(t.Turns, transcriptTurn(i+1, at))
	}
	for {
		raw, err := json.Marshal(t)
		if err != nil {
			return nil, fmt.Errorf("encode the proof transcript: %w", err)
		}
		if len(raw) <= MaxTranscriptBytes {
			return raw, nil
		}
		if !t.shrink() {
			return nil, fmt.Errorf("encode the proof transcript: %d bytes is over the %d byte limit", len(raw), MaxTranscriptBytes)
		}
	}
}

// shrink leaves out one thing: the text of the oldest turn that still has one, then the oldest turn, and last the
// text of the newest turn. It reports false when nothing is left to leave out.
func (t *ProofTranscript) shrink() bool {
	last := len(t.Turns) - 1
	for i := range max(last, 0) {
		if t.Turns[i].Text != "" {
			t.Turns[i].Text = ""
			t.TextsOmitted++
			return true
		}
	}
	switch {
	case last >= 1:
		if t.Turns[0].TextBytes > 0 {
			// Its text was counted as omitted; the whole turn is now counted instead.
			t.TextsOmitted--
		}
		t.Turns = t.Turns[1:]
		t.TurnsOmitted++
		return true
	case last == 0 && t.Turns[0].Text != "":
		t.Turns[0].Text = ""
		t.TextsOmitted++
		return true
	}
	return false
}

func transcriptTurn(n int, at ports.AgentTurn) TranscriptTurn {
	tt := TranscriptTurn{N: n, RequestBytes: at.RequestBytes, TrimmedResults: at.TrimmedResults,
		ToolCalls: at.ToolCalls, Finish: at.Finish, Nudged: at.Nudged, StallNudged: at.StallNudged,
		Attempts: at.Attempts, CostUSD: at.CostUSD, InputTokens: at.Usage.InputTokens,
		OutputTokens: at.Usage.OutputTokens, ReasoningBytes: at.ReasoningBytes, TextBytes: len(at.Text),
		Text: clip(at.Text, MaxTranscriptText)}
	for _, c := range at.Calls[:min(len(at.Calls), maxTranscriptCalls)] {
		tt.Calls = append(tt.Calls, TranscriptCall{Name: clip(c.Name, maxToolName), ArgumentBytes: c.ArgumentBytes,
			Failed: c.Failed, Error: clip(c.Error, maxTranscriptToolError)})
	}
	return tt
}

// maxToolName bounds a tool name in the transcript: a model can call a name no tool has. maxTranscriptCalls bounds
// the calls of one turn, as the loop's own record does; ToolCalls keeps the full count. maxTranscriptToolError bounds
// the error of one failed call, which the loop already cuts to turnToolErrorBytes.
const (
	maxToolName            = 64
	maxTranscriptCalls     = 16
	maxTranscriptToolError = 300
)
