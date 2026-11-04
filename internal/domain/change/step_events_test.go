package change_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
)

// TestStepRetriedValidationNamesEveryBadField refuses each bad payload and names the field that is wrong.
func TestStepRetriedValidationNamesEveryBadField(t *testing.T) {
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
	none := `{"step":"write_tests","attempt":2,"reasons":[]}`
	eight := `{"step":"write_tests","attempt":2,"reasons":["a","b","c","d","e","f","g","h"]}`
	for _, raw := range []string{none, eight} {
		_, err := change.Decode(eventWith(change.EventStepRetried, raw))
		require.NoError(t, err, raw)
	}
}

// TestStepEscalatedValidationNamesEveryBadField refuses each bad payload and names the field that is wrong.
func TestStepEscalatedValidationNamesEveryBadField(t *testing.T) {
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

// TestStepEventsLeaveEveryLiveStateWhereItIs and TestStepEventsAreRefusedBeforeAChangeStarts pin the reducer rule: a
// retry or an escalation is a fact about a live change, so it moves nothing and needs a stream to exist.
func TestStepEventsLeaveEveryLiveStateWhereItIs(t *testing.T) {
	for _, s := range change.AllStates() {
		if s.IsTerminal() {
			continue
		}
		for _, et := range []change.EventType{change.EventStepRetried, change.EventStepEscalated} {
			next, err := change.Reduce(s, eventWith(et, string(goldenPayload(t, et))))
			require.NoError(t, err, "%s + %s", s, et)
			require.Equal(t, s, next, "%s + %s must leave the state unchanged", s, et)
		}
	}
}

func TestStepEventsAreRefusedBeforeAChangeStarts(t *testing.T) {
	for _, et := range []change.EventType{change.EventStepRetried, change.EventStepEscalated} {
		_, err := change.Reduce("", eventWith(et, string(goldenPayload(t, et))))
		require.ErrorIs(t, err, change.ErrIllegalTransition, et)
	}
}
