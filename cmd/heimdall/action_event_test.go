package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

const (
	evHead = id.SHA("1111111111111111111111111111111111111111")
	evBase = id.SHA("2222222222222222222222222222222222222222")
)

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func prEvent(number int, head id.SHA, headRepo, login string) map[string]any {
	return map[string]any{
		"number": number,
		"pull_request": map[string]any{
			"number": number,
			"user":   map[string]any{"login": login},
			"head":   map[string]any{"sha": string(head), "repo": map[string]any{"full_name": headRepo}},
			"base":   map[string]any{"sha": string(evBase), "repo": map[string]any{"full_name": "acme/pay"}},
		},
	}
}

func TestTriggerRefusesWhatItCannotRecord(t *testing.T) {
	cases := map[string]struct {
		name string
		raw  []byte
	}{
		"no pull request": {"pull_request", []byte(`{"number": 7}`)},
		"a bad head":      {"pull_request", mustJSON(t, prEvent(7, "nothex", "acme/pay", "ana"))},
		"a zero number":   {"pull_request", mustJSON(t, prEvent(0, evHead, "acme/pay", "ana"))},
		"another event":   {"push", mustJSON(t, prEvent(7, evHead, "acme/pay", "ana"))},
		"not JSON":        {"pull_request", []byte(`{`)},
		"two JSON values": {"pull_request", append(mustJSON(t, prEvent(7, evHead, "acme/pay", "ana")), []byte(`{}`)...)},
	}
	for label, tc := range cases {
		if _, err := triggerFromEvent(tc.name, tc.raw); err == nil {
			t.Errorf("%s: accepted", label)
		}
	}
}

func TestTriggerRecordRoundTripsAndRefusesExtraFields(t *testing.T) {
	rec := triggerRecord{Version: triggerVersion, Event: "pull_request", Number: 7, Head: evHead}
	raw, err := encodeTriggerRecord(rec)
	if err != nil {
		t.Fatal(err)
	}
	back, err := parseTriggerRecord(raw)
	if err != nil || back != rec {
		t.Fatalf("round trip = %+v, %v", back, err)
	}
	for label, bad := range map[string]string{
		"extra field":   `{"version":1,"event":"pull_request","number":7,"head":"` + string(evHead) + `","admin":true}`,
		"old version":   `{"version":0,"event":"pull_request","number":7,"head":"` + string(evHead) + `"}`,
		"bad head":      `{"version":1,"event":"pull_request","number":7,"head":"zz"}`,
		"huge number":   `{"version":1,"event":"pull_request","number":10000000000,"head":"` + string(evHead) + `"}`,
		"too large":     `{"version":1,"event":"` + strings.Repeat("x", maxTriggerBytes) + `"}`,
		"unknown event": `{"version":1,"event":"push","number":7,"head":"` + string(evHead) + `"}`,
	} {
		if _, err := parseTriggerRecord([]byte(bad)); err == nil {
			t.Errorf("%s: accepted", label)
		}
	}
}

func workflowRunEvent(path, event string, head id.SHA, prs ...int) map[string]any {
	list := []any{}
	for _, n := range prs {
		list = append(list, map[string]any{"number": n})
	}
	return map[string]any{"workflow_run": map[string]any{
		"id": 991, "path": path, "event": event, "head_sha": string(head), "conclusion": "success", "pull_requests": list,
	}}
}

func TestGateEventReadsAWorkflowRun(t *testing.T) {
	ev, err := parseGateEvent("workflow_run", mustJSON(t, workflowRunEvent(proofWorkflowPath, "pull_request", evHead, 7)))
	if err != nil {
		t.Fatal(err)
	}
	if ev.Run == nil || ev.Run.ID != 991 || ev.Run.Path != proofWorkflowPath || ev.Run.HeadSHA != evHead || len(ev.Run.PRs) != 1 || ev.Run.PRs[0] != 7 {
		t.Errorf("event = %+v run = %+v", ev, ev.Run)
	}
}

func TestGateEventReadsACommentOnAPullRequestOnly(t *testing.T) {
	ev, err := parseGateEvent("issue_comment", []byte(`{"issue": {"number": 7, "pull_request": {"url": "x"}}}`))
	if err != nil || ev.Number != 7 {
		t.Fatalf("event = %+v, %v", ev, err)
	}
	ev, err = parseGateEvent("issue_comment", []byte(`{"issue": {"number": 8}}`))
	if err != nil || ev.Number != 0 {
		t.Fatalf("a comment on an issue must name no pull request: %+v, %v", ev, err)
	}
}

func TestGateEventKnowsTheSweepAndRefusesOthers(t *testing.T) {
	ev, err := parseGateEvent("schedule", []byte(`{"schedule": "*/30 * * * *"}`))
	if err != nil || !ev.Sweep {
		t.Fatalf("schedule = %+v, %v", ev, err)
	}
	if _, err := parseGateEvent("pull_request_target", []byte(`{}`)); err == nil {
		t.Error("pull_request_target was accepted: the gate must never run on it (ACTION_MODE 3.1)")
	}
	if _, err := parseGateEvent("workflow_run", []byte(`{"workflow_run": {"id": 0}}`)); err == nil {
		t.Error("a workflow_run with no id was accepted")
	}
}

// ACTION_MODE 2.7: the gate reads a proof artifact only from the proof workflow, run on a pull_request event, for the
// head the gate serves, and only when the run's own trigger record names the same head.
func TestProofArtifactTrust(t *testing.T) {
	good := workflowRun{ID: 991, Path: proofWorkflowPath, Event: "pull_request", HeadSHA: evHead}
	rec := triggerRecord{Version: triggerVersion, Event: "pull_request", Number: 7, Head: evHead}
	if why := proofUntrusted(good, rec, evHead); why != "" {
		t.Fatalf("the good run was refused: %s", why)
	}
	cases := map[string]struct {
		run  workflowRun
		rec  triggerRecord
		head id.SHA
	}{
		"another workflow":          {workflowRun{ID: 991, Path: ".github/workflows/ci.yml", Event: "pull_request", HeadSHA: evHead}, rec, evHead},
		"a pull_request_target run": {workflowRun{ID: 991, Path: proofWorkflowPath, Event: "pull_request_target", HeadSHA: evHead}, rec, evHead},
		"an old head":               {good, rec, evBase},
		"a record of another head":  {good, triggerRecord{Version: 1, Event: "pull_request", Number: 7, Head: evBase}, evHead},
		"a run on another head":     {workflowRun{ID: 991, Path: proofWorkflowPath, Event: "pull_request", HeadSHA: evBase}, rec, evHead},
	}
	for label, tc := range cases {
		if why := proofUntrusted(tc.run, tc.rec, tc.head); why == "" {
			t.Errorf("%s: trusted", label)
		}
	}
}

func TestProofEventReadsForkAndDependabot(t *testing.T) {
	ev, err := parseProofEvent(mustJSON(t, prEvent(7, evHead, "acme/pay", "ana")), "acme/pay")
	if err != nil || ev.Number != 7 || ev.Head != evHead || ev.NoKeyByRule {
		t.Fatalf("same-repo event = %+v, %v", ev, err)
	}
	ev, err = parseProofEvent(mustJSON(t, prEvent(7, evHead, "mallory/pay", "mallory")), "acme/pay")
	if err != nil || !ev.NoKeyByRule {
		t.Fatalf("a fork must get no key by rule: %+v, %v", ev, err)
	}
	ev, err = parseProofEvent(mustJSON(t, prEvent(7, evHead, "acme/pay", "dependabot[bot]")), "acme/pay")
	if err != nil || !ev.NoKeyByRule {
		t.Fatalf("dependabot must get no key by rule: %+v, %v", ev, err)
	}
}
