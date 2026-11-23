package change_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
)

// TestStepRetriedValidationNamesEveryBadField refuses each bad payload and names the field that is wrong.
func TestStepRetriedValidationNamesEveryBadField(t *testing.T) {
	t.Parallel()
	cases := map[string]struct{ raw, want string }{
		"attempt zero":     {`{"step":"write_tests","attempt":0,"reasons":[]}`, "attempt:"},
		"attempt one":      {`{"step":"write_tests","attempt":1,"reasons":[]}`, "attempt:"},
		"empty step":       {`{"step":"","attempt":2,"reasons":[]}`, "step:"},
		"blank step":       {`{"step":"  ","attempt":2,"reasons":[]}`, "step:"},
		"nine reasons":     {`{"step":"write_tests","attempt":2,"reasons":["a","b","c","d","e","f","g","h","i"]}`, "reasons:"},
		"blank reason":     {`{"step":"write_tests","attempt":2,"reasons":["ok"," "]}`, "reasons[1]:"},
		"unknown field":    {`{"step":"write_tests","attempt":2,"reasons":[],"budget":3}`, "unknown field"},
		"missing attempts": {`{"step":"write_tests","reasons":[]}`, "attempt:"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := change.Decode(eventWith(change.EventStepRetried, c.raw))
			require.Error(t, err)
			require.Contains(t, err.Error(), c.want)
		})
	}
}

// TestStepRetriedAcceptsZeroToEightReasons is the other side of the bound: no reasons and eight reasons are both valid.
func TestStepRetriedAcceptsZeroToEightReasons(t *testing.T) {
	t.Parallel()
	none := `{"step":"write_tests","attempt":2,"reasons":[]}`
	eight := `{"step":"write_tests","attempt":2,"reasons":["a","b","c","d","e","f","g","h"]}`
	for _, raw := range []string{none, eight} {
		_, err := change.Decode(eventWith(change.EventStepRetried, raw))
		require.NoError(t, err, raw)
	}
}

// TestStepEscalatedValidationNamesEveryBadField refuses each bad payload and names the field that is wrong.
func TestStepEscalatedValidationNamesEveryBadField(t *testing.T) {
	t.Parallel()
	cases := map[string]struct{ raw, want string }{
		"attempts zero": {`{"step":"write_tests","attempts":0,"reason":"budget used up"}`, "attempts:"},
		"empty step":    {`{"step":"","attempts":3,"reason":"budget used up"}`, "step:"},
		"blank reason":  {`{"step":"write_tests","attempts":3,"reason":" "}`, "reason:"},
		"unknown field": {`{"step":"write_tests","attempts":3,"reason":"budget used up","cost":9}`, "unknown field"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := change.Decode(eventWith(change.EventStepEscalated, c.raw))
			require.Error(t, err)
			require.Contains(t, err.Error(), c.want)
		})
	}
}

// TestStepEventsEncodeAndDecodeToTheSameValue checks the JSON round trip of both payloads.
func TestStepEventsEncodeAndDecodeToTheSameValue(t *testing.T) {
	t.Parallel()
	retried := change.StepRetried{Step: "write_tests", Attempt: 2, Reasons: []string{"does not compile"}}
	raw, err := json.Marshal(retried)
	require.NoError(t, err)
	got, err := change.Decode(eventWith(change.EventStepRetried, string(raw)))
	require.NoError(t, err)
	require.Equal(t, retried, got)

	escalated := change.StepEscalated{Step: "write_tests", Attempts: 3, Reason: "tests still fail"}
	raw, err = json.Marshal(escalated)
	require.NoError(t, err)
	got, err = change.Decode(eventWith(change.EventStepEscalated, string(raw)))
	require.NoError(t, err)
	require.Equal(t, escalated, got)
}

// A retry or an escalation is a fact about the work (TECH_SPEC 3.2): it folds without error and changes nothing a
// person or Missing reads.
func TestStepEventsFoldAndChangeNothingInTheView(t *testing.T) {
	t.Parallel()
	for _, et := range []change.EventType{change.EventStepRetried, change.EventStepEscalated} {
		v, err := change.Fold([]change.Event{eventWith(et, string(goldenPayload(t, et)))})
		require.NoError(t, err, et)
		empty, err := change.Fold(nil)
		require.NoError(t, err)
		require.Equal(t, empty, v, et)
	}
}
