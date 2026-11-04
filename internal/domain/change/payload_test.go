package change_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
)

// goldenPayload is the frozen example for one event type. It is generated once on purpose and read here.
func goldenPayload(t *testing.T, et change.EventType) json.RawMessage {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "payloads", string(et)+".json"))
	require.NoError(t, err, "golden example for %s", et)
	return b
}

// eventWith is an envelope of type et carrying raw as its payload; the rest is valid.
func eventWith(et change.EventType, raw string) change.Event {
	e := envelopeOf(et)
	e.Payload = json.RawMessage(raw)
	return e
}

func envelopeOf(et change.EventType) change.Event {
	return change.Event{
		ID:            "3f1c9a52-8d2b-4e7a-9c11-0b6f5d4e3a21",
		ChangeID:      "PAY-142",
		Version:       1,
		Type:          et,
		Actor:         change.Actor{Kind: change.ActorSystem, ID: "engine"},
		OccurredAt:    time.Date(2026, 11, 4, 9, 30, 0, 0, time.UTC),
		CorrelationID: "trace-1",
	}
}

func TestEveryCatalogTypeHasATypedPayloadAndAGoldenExample(t *testing.T) {
	for _, et := range change.All() {
		p, ok := change.PayloadFor(et)
		require.True(t, ok, "no payload type for %s", et)
		raw := goldenPayload(t, et)
		e := envelopeOf(et)
		e.Payload = raw
		got, err := change.Decode(e)
		require.NoError(t, err, "golden example of %s must decode and validate", et)
		require.Equal(t, reflect.TypeOf(p), reflect.TypeOf(got), "%s decodes to its registered type", et)
		require.NoError(t, e.Validate(), et)
		out, err := json.Marshal(got)
		require.NoError(t, err)
		require.JSONEq(t, string(raw), string(out), "golden JSON of %s: the wire format is frozen", et)
	}
}

func TestPayloadForRefusesUnknownTypes(t *testing.T) {
	_, ok := change.PayloadFor("change.exploded")
	require.False(t, ok)
}

func TestDecodeReturnsTheConcreteTypeAndItsFields(t *testing.T) {
	e := envelopeOf(change.EventTaskAssigned)
	e.Payload = goldenPayload(t, change.EventTaskAssigned)
	got, err := change.Decode(e)
	require.NoError(t, err)
	ta, ok := got.(change.TaskAssigned)
	require.True(t, ok, "typed value, not a map")
	require.Equal(t, "tsk_01HZX3K9QW7M2P5R8T0V4YB6CD", ta.TaskID.String())
	require.Equal(t, "bob-reviewer", ta.Agent.String())
}

// Cases whose field is "" fail while a typed id is decoded (the id package validates in UnmarshalText), so the
// decoder reports the id without a field path. The Validate path names every field; TestPayloadValidateJoinsEvery
// Problem covers that path, and the rest of the table names fields where Validate is reached.
func TestDecodeRefusesInvalidPayloadsNamingTheField(t *testing.T) {
	cases := []struct {
		name  string
		et    change.EventType
		raw   string
		field string
	}{
		{"ticket is a PR stream", change.EventChangeTriggered, `{"ticket":"pr-acme-pay-demo-7","repo":"acme/pay-demo","assignee":"bob","source":"jira"}`, "ticket"},
		{"unknown source", change.EventChangeTriggered, `{"ticket":"PAY-142","repo":"acme/pay-demo","assignee":"bob","source":"slack"}`, "source"},
		{"no assignee", change.EventChangeTriggered, `{"ticket":"PAY-142","repo":"acme/pay-demo","assignee":"","source":"jira"}`, ""},
		{"bad base sha", change.EventBranchEnsured, `{"repo":"acme/pay-demo","branch":"PAY-142-tests","base_sha":"XYZ"}`, ""},
		{"empty branch", change.EventBranchEnsured, `{"repo":"acme/pay-demo","branch":"","base_sha":"a01bcde"}`, "branch"},
		{"unknown role", change.EventTaskAssigned, `{"task_id":"tsk_01HZX3K9QW7M2P5R8T0V4YB6CD","agent":"bob-reviewer","role":"wizard","deadline":"2026-11-05T14:00:00Z","bundle_ref":"blob:sha256:` + zeros64 + `"}`, "role"},
		{"no deadline", change.EventTaskAssigned, `{"task_id":"tsk_01HZX3K9QW7M2P5R8T0V4YB6CD","agent":"bob-reviewer","role":"reviewer","bundle_ref":"blob:sha256:` + zeros64 + `"}`, "deadline"},
		{"task id is a finding", change.EventTaskAcked, `{"task_id":"fnd_01HZX3K9QW7M2P5R8T0V4YB6CD","agent":"bob-reviewer","role":"reviewer"}`, ""},
		{"blank timeout reason", change.EventTaskTimedOut, `{"task_id":"tsk_01HZX3K9QW7M2P5R8T0V4YB6CD","agent":"bob-reviewer","role":"reviewer","reason":""}`, "reason"},
		{"empty question ref", change.EventQuestionAsked, `{"question_id":"q-1","refs":[""],"sender":"s-1","message_id":"m-1"}`, "refs[0]"},
		{"AC key in lower case", change.EventTestsSubmitted, `{"refs":["tests/heimdall/PAY-142/a_test.go"],"ac_map":{"ac1":["TestA"]},"sha":"9f1c2ab","tree_hash":"7d3e9a1"}`, "ac_map"},
		{"run id not a run", change.EventTestsVerified, `{"run_ids":["run-1"],"sha":"9f1c2ab","tree_hash":"7d3e9a1"}`, ""},
		{"rejection without reasons", change.EventTestsRejected, `{"reasons":[],"run_ids":[],"sha":"9f1c2ab"}`, "reasons"},
		{"no approver", change.EventTestsApproved, `{"approver":"","tree_hash":"7d3e9a1"}`, ""},
		{"slop passed with failing rules", change.EventSlopEvaluated, `{"pass":true,"failing_rules":["ticket_key"]}`, "failing_rules"},
		{"slop failed without a rule", change.EventSlopEvaluated, `{"pass":false,"failing_rules":[]}`, "failing_rules"},
		{"unknown pick method", change.EventReviewersPicked, `{"people":["bob"],"by":"alice","method":"magic"}`, "method"},
		{"no people picked", change.EventReviewersPicked, `{"people":[],"by":"alice","method":"command"}`, "people"},
		{"impossible pack date", change.EventReviewSubmitted, `{"reviewer":"bob-reviewer","finding_refs":["F1"],"pack_date":"2026-13-40"}`, "pack_date"},
		{"waiver without approver", change.EventReviewWaived, `{"reviewer":"bob-reviewer","reason":"out sick","approver":""}`, ""},
		{"unknown tier", change.EventFindingFiled, `{"finding_id":"fnd_01HZX3K9QW7M2P5R8T0V4YB6CD","ref":"F1","tier":"E9","state":"open","by":"bob-reviewer"}`, "tier"},
		{"unknown finding state", change.EventFindingStateChanged, `{"finding_id":"fnd_01HZX3K9QW7M2P5R8T0V4YB6CD","ref":"F1","tier":"E1","state":"limbo","by":"bob-reviewer"}`, "state"},
		{"challenge round three", change.EventChallengeFiled, `{"finding":"F1","round":3,"evidence":"tests/heimdall/PAY-142/a_test.go"}`, "round"},
		{"applied verdict without citations", change.EventVerdictApplied, `{"case_id":"case_01HZX3K9QW7M2P5R8T0V4YB6CD","verdict":"uphold","citations":[],"checks":["reverified"]}`, "citations"},
		{"unknown verdict", change.EventVerdictSubmitted, `{"case_id":"case_01HZX3K9QW7M2P5R8T0V4YB6CD","verdict":"maybe","citations":["run_01HZX3K9QW7M2P5R8T0V4YB6CD"],"checks":[]}`, "verdict"},
		{"unknown decision kind", change.EventDecisionRecorded, `{"kind":"yolo","target":"F1","login":"bob","reason":"ok","source_ref":"comment-9"}`, "kind"},
		{"decision with no reason", change.EventDecisionRecorded, `{"kind":"dismiss","target":"F1","login":"bob","reason":"","source_ref":"comment-9"}`, "reason"},
		{"approval on a short commit", change.EventApprovalRecorded, `{"login":"rekdfr","commit_sha":"abc"}`, ""},
		{"pending is not a conclusion", change.EventCIReported, `{"check":"heimdall/acceptance","sha":"9f1c2ab","conclusion":"pending"}`, "conclusion"},
		{"ok with reasons", change.EventGateEvaluated, `{"ok":true,"reasons":["ci_red"],"sha":"9f1c2ab","tree_hash":"7d3e9a1","policy_version":3}`, "reasons"},
		{"gate without tree hash", change.EventGateEvaluated, `{"ok":true,"reasons":[],"sha":"9f1c2ab","policy_version":3}`, "tree_hash"},
		{"linked to a PR stream", change.EventPRLinked, `{"repo":"acme/pay-demo","number":7,"ticket":"pr-acme-pay-demo-7"}`, "ticket"},
		{"PR number zero", change.EventPROpened, `{"repo":"acme/pay-demo","number":0,"head_sha":"9f1c2ab","author":"bob"}`, "number"},
		{"merge sha too short", change.EventChangeMerged, `{"sha":"abc"}`, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := change.Decode(eventWith(c.et, c.raw))
			require.Error(t, err)
			if c.field != "" {
				require.Contains(t, err.Error(), c.field)
			}
		})
	}
}

const zeros64 = "0000000000000000000000000000000000000000000000000000000000000000"

func TestPayloadValidateJoinsEveryProblem(t *testing.T) {
	p := change.Triggered{Ticket: "pr-acme-pay-demo-7", Repo: "acme", Source: "slack"}
	err := p.Validate()
	require.Error(t, err)
	for _, field := range []string{"ticket", "repo", "assignee", "source"} {
		require.Contains(t, err.Error(), field)
	}
}

func TestDecodeIsStrictAboutUnknownFieldsDuplicatesAndTrailingData(t *testing.T) {
	for name, raw := range map[string]string{
		"unknown field":    `{"reason":"x","extra":1}`,
		"duplicate key":    `{"reason":"x","reason":"y"}`,
		"trailing object":  `{"reason":"x"} {}`,
		"fractional value": `{"reason":"x","n":1.5}`,
		"not an object":    `"x"`,
		"empty":            ``,
	} {
		_, err := change.Decode(eventWith(change.EventChangeRejected, raw))
		require.Error(t, err, name)
	}
}

func TestDecodeRefusesInvariantOneForAgentsOnDecisionsAndApprovals(t *testing.T) {
	e := eventWith(change.EventApprovalRecorded, `{"login":"rekdfr","commit_sha":"9f1c2ab"}`)
	e.Actor = change.Actor{Kind: change.ActorAgent, ID: "test-author"}
	_, err := change.Decode(e)
	require.Error(t, err)
	require.Contains(t, strings.ToLower(err.Error()), "invariant 1")
	e.Actor = change.Actor{Kind: change.ActorHuman, ID: "rekdfr"}
	_, err = change.Decode(e)
	require.NoError(t, err, "a human approval is valid")
}

func TestEveryTriggerSourceIsAccepted(t *testing.T) {
	for _, src := range []string{"jira", "file", "pr", "manual"} {
		raw := `{"ticket":"PAY-142","repo":"acme/pay-demo","assignee":"bob","source":"` + src + `"}`
		_, err := change.Decode(eventWith(change.EventChangeTriggered, raw))
		require.NoError(t, err, "trigger source %s is in TECH_SPEC 2.5 and D146", src)
	}
}

func TestPayloadsAcceptTheSwitchAndGitHubIdentifiersAsOpaqueText(t *testing.T) {
	_, err := change.Decode(eventWith(change.EventQuestionAnswered, `{"question_id":"q-1","sender":"sw-88:alice","message_id":"msg_0042"}`))
	require.NoError(t, err)
}

func FuzzEventDecodeAndValidateNeverPanics(f *testing.F) {
	for _, et := range change.All() {
		e := envelopeOf(et)
		e.Payload = json.RawMessage(`{}`)
		b, _ := json.Marshal(e)
		f.Add(b)
	}
	f.Add([]byte(`{"type":"change.rejected","payload":null}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		var e change.Event
		if err := json.Unmarshal(data, &e); err != nil {
			return
		}
		_ = e.Validate()
		if _, err := change.Decode(e); err == nil {
			out, err := json.Marshal(e)
			require.NoError(t, err)
			var back change.Event
			require.NoError(t, json.Unmarshal(out, &back))
		}
	})
}

func FuzzDecodePayloadNeverPanicsAndRoundTrips(f *testing.F) {
	for _, et := range change.All() {
		f.Add(0, string(goldenPayloadFile(f, et)))
	}
	f.Fuzz(func(t *testing.T, idx int, raw string) {
		types := change.All()
		if idx < 0 {
			idx = -idx
		}
		et := types[idx%len(types)]
		e := envelopeOf(et)
		e.Payload = json.RawMessage(raw)
		p, err := change.Decode(e)
		if err != nil {
			return
		}
		out, err := json.Marshal(p)
		require.NoError(t, err)
		e.Payload = out
		again, err := change.Decode(e)
		require.NoError(t, err, "a valid payload re-encodes to a valid payload")
		require.Equal(t, p, again)
	})
}

func goldenPayloadFile(f *testing.F, et change.EventType) []byte {
	b, err := os.ReadFile(filepath.Join("testdata", "payloads", string(et)+".json"))
	require.NoError(f, err)
	return b
}
