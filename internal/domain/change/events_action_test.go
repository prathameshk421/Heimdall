package change_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/fastcheck"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/gate"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/proof"
)

const aHash = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

// actionGood is one valid payload per Action event, the starting point of every case below.
func actionGood() map[change.EventType]string {
	return map[change.EventType]string{
		change.EventFastReported: `{"head":"9f1c2ab","verdict":"fail","clauses":[` +
			`{"id":"secrets","status":"met","reason":"no new secret in the added lines"},` +
			`{"id":"approval","status":"unmet","reason":"no approval on head"}]}`,
		change.EventProofStarted: `{"workflow_run":1234,"head":"9f1c2ab","base":"7d3e9a1","policy_hash":"` + aHash + `","model":"deepseek/deepseek-v4-flash"}`,
		change.EventProofReported: `{"workflow_run":1234,"head":"9f1c2ab","base":"7d3e9a1","policy_hash":"` + aHash + `","model":"deepseek/deepseek-v4-flash",` +
			`"kind":"blocked","counts":{"in":4,"proven":2,"blocking":1,"needs_person":1,"refuted":1,"no_proof":0,"outdated":0,"no_location":0,"not_checked":1},` +
			`"cost":{"usd_micros":240000,"prompt_tokens":120000,"completion_tokens":9000},` +
			`"results":[{"ref":"F1","class":"proven","blocking":true},{"ref":"F2","class":"proven"},` +
			`{"ref":"F3","class":"dropped","dropped":"refuted"},{"ref":"F4","class":"not_checked","reason":"budget"}]}`,
		change.EventCostRecorded: `{"workflow_run":1234,"model":"deepseek/deepseek-v4-flash","prompt_tokens":120000,"completion_tokens":9000,"usd_micros":240000}`,
		change.EventExternalIngested: `{"source":"coderabbit","comment_id":1234567,"head":"9f1c2ab","location":{"file":"pay/refund.go","lines":[40,44]},` +
			`"side":"RIGHT","claim":"The refund amount is not checked against the charge.","severity":"high"}`,
		change.EventSummaryPosted:      `{"head":"9f1c2ab","comment_id":987654,"marker":"<!-- heimdall:change=pr-acme-pay-7;version=12;head=9f1c2ab -->"}`,
		change.EventBreakGlassRecorded: `{"head":"9f1c2ab","login":"alice","reason":"production incident, rollback blocked","source_ref":"comment-55"}`,
		change.EventBreakGlassReviewed: `{"head":"9f1c2ab","login":"bob","source_ref":"comment-61"}`,
		change.EventStateReset:         `{"reason":"state log failed its MAC check"}`,
	}
}

// actionRequired lists the fields whose absence must be refused, per event. A count or a token total that is absent is
// zero, which is a valid value, so it is not listed.
func actionRequired() map[change.EventType][]string {
	return map[change.EventType][]string{
		change.EventFastReported:       {"head", "verdict", "clauses"},
		change.EventProofStarted:       {"workflow_run", "head", "base", "policy_hash", "model"},
		change.EventProofReported:      {"workflow_run", "head", "base", "policy_hash", "kind"},
		change.EventCostRecorded:       {"workflow_run", "model"},
		change.EventExternalIngested:   {"source", "comment_id", "head", "location", "side", "claim", "severity"},
		change.EventSummaryPosted:      {"head", "comment_id", "marker"},
		change.EventBreakGlassRecorded: {"head", "login", "reason", "source_ref"},
		change.EventBreakGlassReviewed: {"head", "login", "source_ref"},
		change.EventStateReset:         {"reason"},
	}
}

func actionTypes() []change.EventType {
	return []change.EventType{
		change.EventFastReported, change.EventProofStarted, change.EventProofReported, change.EventCostRecorded,
		change.EventExternalIngested, change.EventSummaryPosted, change.EventBreakGlassRecorded,
		change.EventBreakGlassReviewed, change.EventStateReset,
	}
}

func TestTheActionEventNamesAreTheDesignNames(t *testing.T) {
	t.Parallel()
	for typ, want := range map[change.EventType]string{
		change.EventProofStarted:       "proof.started",
		change.EventProofReported:      "proof.reported",
		change.EventCostRecorded:       "cost.recorded",
		change.EventExternalIngested:   "external.ingested",
		change.EventBreakGlassRecorded: "breakglass.recorded",
		change.EventBreakGlassReviewed: "breakglass.reviewed",
		change.EventStateReset:         "state.reset",
		change.EventFastReported:       "fast.reported",
		change.EventSummaryPosted:      "summary.posted",
	} {
		require.Equal(t, want, string(typ))
		require.True(t, typ.Valid(), want)
	}
	require.Len(t, actionTypes(), 9)
}

func TestEveryActionEventIsInTheCatalogAfterTheServerEvents(t *testing.T) {
	t.Parallel()
	all := change.All()
	require.Equal(t, change.EventChangeMerged, all[54], "the 55 server events come first")
	require.Equal(t, actionTypes(), all[55:])
}

func TestEveryActionPayloadRoundTripsByteForByte(t *testing.T) {
	t.Parallel()
	for _, typ := range actionTypes() {
		raw := actionGood()[typ]
		got, err := change.Decode(eventWith(typ, raw))
		require.NoError(t, err, typ)
		want, ok := change.PayloadFor(typ)
		require.True(t, ok, typ)
		require.IsType(t, want, got, typ)
		out, err := json.Marshal(got)
		require.NoError(t, err)
		require.JSONEq(t, raw, string(out), "%s: the wire format is frozen", typ)
		require.NoError(t, eventWith(typ, raw).Validate(), typ)
	}
}

func TestAnActionPayloadWithAFieldRemovedIsRefused(t *testing.T) {
	t.Parallel()
	for typ, fields := range actionRequired() {
		for _, field := range fields {
			var obj map[string]json.RawMessage
			require.NoError(t, json.Unmarshal([]byte(actionGood()[typ]), &obj))
			require.Contains(t, obj, field, "the good %s payload has %s", typ, field)
			delete(obj, field)
			raw, err := json.Marshal(obj)
			require.NoError(t, err)
			_, err = change.Decode(eventWith(typ, string(raw)))
			require.Error(t, err, "%s without %s", typ, field)
		}
	}
}

func TestAnActionPayloadWithAnUnknownFieldIsRefused(t *testing.T) {
	t.Parallel()
	for _, typ := range actionTypes() {
		raw := actionGood()[typ]
		raw = `{"extra":1,` + strings.TrimPrefix(raw, "{")
		_, err := change.Decode(eventWith(typ, raw))
		require.Error(t, err, typ)
	}
}

// sub returns raw with the first old replaced by next. The old text must be there, so a typo in a case fails the case.
func sub(t *testing.T, raw, old, next string) string {
	t.Helper()
	require.Contains(t, raw, old)
	return strings.Replace(raw, old, next, 1)
}

func TestActionPayloadsRefuseBadValuesNamingTheField(t *testing.T) {
	t.Parallel()
	g := actionGood()
	longReason := strings.Repeat("x", 2049)
	cases := []struct {
		name  string
		typ   change.EventType
		raw   string
		field string
	}{
		{"fast: unknown verdict", change.EventFastReported, sub(t, g[change.EventFastReported], `"verdict":"fail"`, `"verdict":"maybe"`), "verdict"},
		{"fast: pass with an unmet clause", change.EventFastReported, sub(t, g[change.EventFastReported], `"verdict":"fail"`, `"verdict":"pass"`), "verdict"},
		{"fast: unknown when a clause is unmet", change.EventFastReported,
			sub(t, g[change.EventFastReported], `"verdict":"fail"`, `"verdict":"unknown"`), "verdict"},
		{"fast: fail when only a clause is unknown", change.EventFastReported,
			`{"head":"9f1c2ab","verdict":"fail","clauses":[{"id":"analyzers","status":"unknown","reason":"results not known"}]}`, "verdict"},
		{"fast: pass with a clause not met", change.EventFastReported,
			`{"head":"9f1c2ab","verdict":"pass","clauses":[{"id":"analyzers","status":"unknown","reason":"results not known"}]}`, "verdict"},
		{"fast: no clause", change.EventFastReported, `{"head":"9f1c2ab","verdict":"pass","clauses":[]}`, "clauses"},
		{"fast: unknown clause id", change.EventFastReported, sub(t, g[change.EventFastReported], `"id":"secrets"`, `"id":"vibes"`), "clauses[0].id"},
		{"fast: bad status", change.EventFastReported, sub(t, g[change.EventFastReported], `"status":"met"`, `"status":"fine"`), "clauses[0].status"},
		{"fast: blank reason", change.EventFastReported, sub(t, g[change.EventFastReported], `"no new secret in the added lines"`, `" "`), "clauses[0].reason"},
		{"fast: reason too long", change.EventFastReported, sub(t, g[change.EventFastReported], `"no new secret in the added lines"`, `"`+longReason+`"`), "clauses[0].reason"},
		{"fast: same clause twice", change.EventFastReported, sub(t, g[change.EventFastReported], `"id":"approval"`, `"id":"secrets"`), "clauses[1].id"},
		{"fast: short head", change.EventFastReported, sub(t, g[change.EventFastReported], `"head":"9f1c2ab"`, `"head":"abc"`), ""},

		{"proof.started: short base", change.EventProofStarted, sub(t, g[change.EventProofStarted], `"base":"7d3e9a1"`, `"base":"zz"`), ""},
		{"proof.started: policy hash not hex", change.EventProofStarted, sub(t, g[change.EventProofStarted], aHash, "nothex"), "policy_hash"},
		{"proof.started: workflow run zero", change.EventProofStarted, sub(t, g[change.EventProofStarted], `"workflow_run":1234`, `"workflow_run":0`), "workflow_run"},
		{"proof.started: blank model", change.EventProofStarted, sub(t, g[change.EventProofStarted], `"model":"deepseek/deepseek-v4-flash"`, `"model":""`), "model"},

		{"proof.reported: workflow run zero", change.EventProofReported, sub(t, g[change.EventProofReported], `"workflow_run":1234`, `"workflow_run":-3`), "workflow_run"},
		{"proof.reported: unknown kind", change.EventProofReported, sub(t, g[change.EventProofReported], `"kind":"blocked"`, `"kind":"great"`), "kind"},
		{"proof.reported: a reason on a blocked run", change.EventProofReported, sub(t, g[change.EventProofReported], `"kind":"blocked"`, `"kind":"blocked","reason":"budget"`), "reason"},
		{"proof.reported: unknown without a reason", change.EventProofReported, sub(t, g[change.EventProofReported], `"kind":"blocked"`, `"kind":"unknown"`), "reason"},
		{"proof.reported: unknown for a reason that cannot stop a run", change.EventProofReported,
			sub(t, g[change.EventProofReported], `"kind":"blocked"`, `"kind":"unknown","reason":"trivial"`), "reason"},
		{"proof.reported: blocked with no blocking finding", change.EventProofReported,
			sub(t, g[change.EventProofReported], `"blocking":1,"needs_person":1`, `"blocking":0,"needs_person":2`), "kind"},
		{"proof.reported: proven with a blocking finding", change.EventProofReported, sub(t, g[change.EventProofReported], `"kind":"blocked"`, `"kind":"proven"`), "kind"},
		{"proof.reported: negative count", change.EventProofReported, sub(t, g[change.EventProofReported], `"refuted":1`, `"refuted":-1`), "counts"},
		{"proof.reported: counts do not add up", change.EventProofReported, sub(t, g[change.EventProofReported], `"in":4`, `"in":5`), "counts"},
		{"proof.reported: blocking and needs-person do not make the proven", change.EventProofReported,
			sub(t, g[change.EventProofReported], `"needs_person":1`, `"needs_person":0`), "counts"},
		{"proof.reported: a skipped run that cost money", change.EventProofReported,
			`{"workflow_run":1,"head":"9f1c2ab","base":"7d3e9a1","policy_hash":"` + aHash + `","kind":"skipped_trivial","counts":{},"cost":{"usd_micros":5}}`, "cost"},
		{"proof.reported: negative cost", change.EventProofReported, sub(t, g[change.EventProofReported], `"usd_micros":240000`, `"usd_micros":-1`), "cost"},
		{"proof.reported: more results than candidates", change.EventProofReported,
			sub(t, g[change.EventProofReported], `"in":4,"proven":2,"blocking":1,"needs_person":1,"refuted":1,"no_proof":0,"outdated":0,"no_location":0,"not_checked":1`,
				`"in":1,"proven":1,"blocking":1,"needs_person":0,"refuted":0,"no_proof":0,"outdated":0,"no_location":0,"not_checked":0`), "results"},
		{"proof.reported: the same finding twice", change.EventProofReported, sub(t, g[change.EventProofReported], `"ref":"F2"`, `"ref":"F1"`), "results[1].ref"},
		{"proof.reported: unknown class", change.EventProofReported, sub(t, g[change.EventProofReported], `"class":"proven","blocking":true`, `"class":"maybe"`), "results[0].class"},
		{"proof.reported: proven result with a drop reason", change.EventProofReported,
			sub(t, g[change.EventProofReported], `{"ref":"F2","class":"proven"}`, `{"ref":"F2","class":"proven","dropped":"refuted"}`), "results[1]"},
		{"proof.reported: dropped result with no reason", change.EventProofReported, sub(t, g[change.EventProofReported], `,"dropped":"refuted"`, ``), "results[2].dropped"},
		{"proof.reported: dropped result that blocks", change.EventProofReported, sub(t, g[change.EventProofReported], `"dropped":"refuted"`, `"dropped":"refuted","blocking":true`), "results[2]"},
		{"proof.reported: not checked for a reason that is not allowed", change.EventProofReported,
			sub(t, g[change.EventProofReported], `"class":"not_checked","reason":"budget"`, `"class":"not_checked","reason":"no_intent"`), "results[3].reason"},
		{"proof.reported: not checked with no reason", change.EventProofReported, sub(t, g[change.EventProofReported], `,"reason":"budget"`, ``), "results[3].reason"},
		{"proof.reported: finding ref zero", change.EventProofReported, sub(t, g[change.EventProofReported], `"ref":"F1"`, `"ref":"F0"`), ""},

		{"cost.recorded: workflow run zero", change.EventCostRecorded, sub(t, g[change.EventCostRecorded], `"workflow_run":1234`, `"workflow_run":0`), "workflow_run"},
		{"cost.recorded: blank model", change.EventCostRecorded, sub(t, g[change.EventCostRecorded], `"model":"deepseek/deepseek-v4-flash"`, `"model":" "`), "model"},
		{"cost.recorded: negative prompt tokens", change.EventCostRecorded, sub(t, g[change.EventCostRecorded], `"prompt_tokens":120000`, `"prompt_tokens":-1`), "prompt_tokens"},
		{"cost.recorded: negative completion tokens", change.EventCostRecorded, sub(t, g[change.EventCostRecorded], `"completion_tokens":9000`, `"completion_tokens":-5`), "completion_tokens"},
		{"cost.recorded: negative usd", change.EventCostRecorded, sub(t, g[change.EventCostRecorded], `"usd_micros":240000`, `"usd_micros":-240000`), "usd_micros"},
		{"cost.recorded: absurd usd", change.EventCostRecorded, sub(t, g[change.EventCostRecorded], `"usd_micros":240000`, `"usd_micros":9000000000000000`), "usd_micros"},
		{"cost.recorded: a fraction of a micro dollar", change.EventCostRecorded, sub(t, g[change.EventCostRecorded], `"usd_micros":240000`, `"usd_micros":0.24`), ""},

		{"external.ingested: unknown source", change.EventExternalIngested, sub(t, g[change.EventExternalIngested], `"coderabbit"`, `"sonarqube"`), "source"},
		{"external.ingested: comment id zero", change.EventExternalIngested, sub(t, g[change.EventExternalIngested], `"comment_id":1234567`, `"comment_id":0`), "comment_id"},
		{"external.ingested: claim too short", change.EventExternalIngested, sub(t, g[change.EventExternalIngested], `"The refund amount is not checked against the charge."`, `"short"`), "claim"},
		{"external.ingested: claim too long", change.EventExternalIngested,
			sub(t, g[change.EventExternalIngested], `"The refund amount is not checked against the charge."`, `"`+strings.Repeat("c", 601)+`"`), "claim"},
		{"external.ingested: unknown severity", change.EventExternalIngested, sub(t, g[change.EventExternalIngested], `"severity":"high"`, `"severity":"urgent"`), "severity"},
		{"external.ingested: lines reversed", change.EventExternalIngested, sub(t, g[change.EventExternalIngested], `[40,44]`, `[44,40]`), "location.lines"},
		{"external.ingested: line zero", change.EventExternalIngested, sub(t, g[change.EventExternalIngested], `[40,44]`, `[0,44]`), "location.lines"},
		{"external.ingested: path outside the repository", change.EventExternalIngested, sub(t, g[change.EventExternalIngested], `"pay/refund.go"`, `"../etc/passwd"`), "location.file"},
		{"external.ingested: unknown side", change.EventExternalIngested, sub(t, g[change.EventExternalIngested], `"side":"RIGHT"`, `"side":"UP"`), "side"},

		{"summary.posted: comment id zero", change.EventSummaryPosted, sub(t, g[change.EventSummaryPosted], `"comment_id":987654`, `"comment_id":0`), "comment_id"},
		{"summary.posted: blank marker", change.EventSummaryPosted, `{"head":"9f1c2ab","comment_id":9,"marker":""}`, "marker"},
		{"summary.posted: short head", change.EventSummaryPosted, sub(t, g[change.EventSummaryPosted], `"head":"9f1c2ab"`, `"head":"x"`), ""},

		{"breakglass.recorded: reason one character short", change.EventBreakGlassRecorded, sub(t, g[change.EventBreakGlassRecorded], `"production incident, rollback blocked"`, `"fourteen chars"`), "reason"},
		{"breakglass.recorded: padded reason", change.EventBreakGlassRecorded, sub(t, g[change.EventBreakGlassRecorded], `"production incident, rollback blocked"`, `"          short          "`), "reason"},
		{"breakglass.recorded: blank login", change.EventBreakGlassRecorded, sub(t, g[change.EventBreakGlassRecorded], `"login":"alice"`, `"login":""`), ""},
		{"breakglass.recorded: blank source", change.EventBreakGlassRecorded, sub(t, g[change.EventBreakGlassRecorded], `"source_ref":"comment-55"`, `"source_ref":" "`), "source_ref"},
		{"breakglass.reviewed: blank login", change.EventBreakGlassReviewed, sub(t, g[change.EventBreakGlassReviewed], `"login":"bob"`, `"login":""`), ""},
		{"breakglass.reviewed: blank source", change.EventBreakGlassReviewed, sub(t, g[change.EventBreakGlassReviewed], `"source_ref":"comment-61"`, `"source_ref":""`), "source_ref"},
		{"state.reset: blank reason", change.EventStateReset, `{"reason":" "}`, "reason"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := change.Decode(eventWith(c.typ, c.raw))
			require.Error(t, err)
			if c.field != "" {
				require.Contains(t, err.Error(), c.field)
			}
		})
	}
}

func TestABreakGlassReasonOfFifteenCharactersIsAccepted(t *testing.T) {
	t.Parallel()
	raw := sub(t, actionGood()[change.EventBreakGlassRecorded], `"production incident, rollback blocked"`, `"fifteen chars!!"`)
	_, err := change.Decode(eventWith(change.EventBreakGlassRecorded, raw))
	require.NoError(t, err)
}

func TestNoAgentCanRecordOrReviewABreakGlass(t *testing.T) {
	t.Parallel()
	for _, typ := range []change.EventType{change.EventBreakGlassRecorded, change.EventBreakGlassReviewed} {
		e := eventWith(typ, actionGood()[typ])
		e.Actor = change.Actor{Kind: change.ActorAgent, ID: "test-author"}
		_, err := change.Decode(e)
		require.Error(t, err, typ)
		require.Contains(t, strings.ToLower(err.Error()), "invariant 1", typ)
		require.Error(t, e.Validate(), typ)
		for _, kind := range []change.ActorKind{change.ActorHuman, change.ActorSystem, change.ActorGitHub} {
			e.Actor = change.Actor{Kind: kind, ID: "gate"}
			_, err = change.Decode(e)
			require.NoError(t, err, "%s from %s", typ, kind)
		}
	}
}

func TestOtherActionEventsMayComeFromAnyActor(t *testing.T) {
	t.Parallel()
	for _, typ := range []change.EventType{change.EventFastReported, change.EventProofReported, change.EventStateReset} {
		e := eventWith(typ, actionGood()[typ])
		e.Actor = change.Actor{Kind: change.ActorAgent, ID: "runner"}
		_, err := change.Decode(e)
		require.NoError(t, err, typ)
	}
}

func TestEveryProofKindAndReasonIsAcceptedWhereItBelongs(t *testing.T) {
	t.Parallel()
	zero := `"counts":{},"cost":{}`
	head := `{"workflow_run":1,"head":"9f1c2ab","base":"7d3e9a1","policy_hash":"` + aHash + `",`
	for _, kind := range []proof.Kind{proof.KindProven, proof.KindNotReproduced} {
		_, err := change.Decode(eventWith(change.EventProofReported, head+`"kind":"`+string(kind)+`",`+zero+`}`))
		require.NoError(t, err, kind)
	}
	_, err := change.Decode(eventWith(change.EventProofReported, head+`"kind":"skipped_trivial",`+zero+`}`))
	require.NoError(t, err)
	for _, r := range proof.StopReasons() {
		_, err := change.Decode(eventWith(change.EventProofReported, head+`"kind":"unknown","reason":"`+string(r)+`",`+zero+`}`))
		require.NoError(t, err, r)
	}
}

func TestNewFastReportedCopiesTheResultAndItsVerdict(t *testing.T) {
	t.Parallel()
	r := fastcheck.Result{
		Clauses: []fastcheck.Clause{
			{ID: fastcheck.ClauseSecrets, Status: fastcheck.StatusMet, Reason: "none"},
			{ID: fastcheck.ClauseAnalyzers, Status: fastcheck.StatusUnknown, Reason: "not known"},
			{ID: fastcheck.ClauseDiffCoverage, Status: fastcheck.StatusNotChecked, Reason: "off"},
		},
		Decision: gate.Decision{OK: false, Unmet: []gate.Clause{{ID: "analyzers", Reason: "not known", Unknown: true}}},
		Verdict:  gate.VerdictUnknown,
	}
	p := change.NewFastReported("9f1c2ab", r)
	require.Equal(t, gate.VerdictUnknown, p.Verdict)
	require.Len(t, p.Clauses, 3)
	require.Equal(t, fastcheck.StatusUnknown, p.Clauses[1].Status)
	require.NoError(t, p.Validate())
}

func TestNewFastReportedClipsAReasonThatIsTooLongAtARuneBoundary(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("é", change.MaxClauseReason) // two bytes each, so the clip falls inside a rune if it is careless
	r := fastcheck.Result{
		Clauses:  []fastcheck.Clause{{ID: fastcheck.ClauseSecrets, Status: fastcheck.StatusUnmet, Reason: long}},
		Decision: gate.Decision{Unmet: []gate.Clause{{ID: "secrets", Reason: long}}},
		Verdict:  gate.VerdictFail,
	}
	p := change.NewFastReported("9f1c2ab", r)
	require.LessOrEqual(t, len(p.Clauses[0].Reason), change.MaxClauseReason)
	require.True(t, strings.HasSuffix(p.Clauses[0].Reason, "…"), "a clipped reason says so")
	require.NoError(t, p.Validate())
}

func TestNewCostRoundsToMicroDollarsAndRefusesAValueThatCannotBeTrue(t *testing.T) {
	t.Parallel()
	c, err := change.NewCost(proof.Spend{USD: 0.2400004, InputTokens: 120000, OutputTokens: 9000})
	require.NoError(t, err)
	require.Equal(t, change.Cost{USDMicros: 240000, PromptTokens: 120000, CompletionTokens: 9000}, c)
	require.Equal(t, proof.Spend{USD: 0.24, InputTokens: 120000, OutputTokens: 9000}, c.Spend())
	for _, bad := range []proof.Spend{{USD: -1}, {USD: 1e13}, {InputTokens: -1}, {OutputTokens: -1}} {
		_, err := change.NewCost(bad)
		require.Error(t, err, "%+v", bad)
	}
	nan := 0.0
	nan /= nan
	_, err = change.NewCost(proof.Spend{USD: nan})
	require.Error(t, err, "not a number")
}

func TestProofCountsConvertBothWays(t *testing.T) {
	t.Parallel()
	in := proof.Counts{In: 40, Proven: 4, Blocking: 1, NeedsPerson: 3, Refuted: 12, NoProof: 15, Outdated: 3, NoLocation: 0, NotChecked: 6}
	got := change.NewProofCounts(in)
	require.Equal(t, in, got.Counts())
	require.NoError(t, got.Validate())
}

func TestAProofOutcomeFromEvaluateIsAlwaysAValidPayload(t *testing.T) {
	t.Parallel()
	// The payload checks must never refuse what proof.Evaluate produces, or a finished run could not be recorded.
	facts := []proof.Facts{
		{Trivial: proof.Decision{Skip: true}},
		{Tests: proof.TestFacts{Ran: true, Accepted: 2}, Findings: []proof.Finding{{Class: proof.ClassProven, Blocking: true}, {Class: proof.ClassDropped, Dropped: proof.DropRefuted}}},
		{Tests: proof.TestFacts{Ran: true, Accepted: 1}, Findings: []proof.Finding{{Class: proof.ClassNotChecked, Reason: proof.ReasonNotProvable}}},
		{Stop: proof.ReasonBudget, Tests: proof.TestFacts{Ran: true}},
		{Stop: proof.ReasonSuperseded, Findings: []proof.Finding{{Class: proof.ClassProven, Blocking: true}}},
		{},
	}
	for i, f := range facts {
		o := proof.Evaluate(f)
		cost, err := change.NewCost(o.Cost)
		require.NoError(t, err)
		p := change.ProofReported{WorkflowRun: 1, Head: "9f1c2ab", Base: "7d3e9a1", PolicyHash: aHash, Kind: o.Kind, Reason: o.Reason,
			Counts: change.NewProofCounts(o.Counts), Cost: cost}
		require.NoError(t, p.Validate(), "facts %d: kind %s", i, o.Kind)
	}
}

func TestProofResultsAreBoundedAtTheCandidateLimit(t *testing.T) {
	t.Parallel()
	var rs []change.CandidateResult
	for i := 1; i <= change.MaxProofResults+1; i++ {
		ref, err := id.NewFindingRef(i)
		require.NoError(t, err)
		rs = append(rs, change.CandidateResult{Ref: ref, Class: proof.ClassProven})
	}
	p := change.ProofReported{WorkflowRun: 1, Head: "9f1c2ab", Base: "7d3e9a1", PolicyHash: aHash, Kind: proof.KindProven,
		Counts: change.ProofCounts{In: len(rs), Proven: len(rs), NeedsPerson: len(rs)}, Results: rs}
	err := p.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "results")
	p.Results, p.Counts = rs[:change.MaxProofResults], change.ProofCounts{In: change.MaxProofResults, Proven: change.MaxProofResults, NeedsPerson: change.MaxProofResults}
	require.NoError(t, p.Validate())
}
