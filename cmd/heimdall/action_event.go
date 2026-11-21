package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

// The names the three workflows share (ACTION_MODE 3.2). The trigger and proof workflows upload their records under
// these artifact names, and the gate reads them back from the run that started it.
const (
	triggerArtifact     = "heimdall-event"
	triggerFile         = "heimdall-event.json"
	proofArtifactName   = "heimdall-proof"
	proofFile           = "heimdall-proof.json"
	proofWorkflowPath   = ".github/workflows/heimdall-proof.yml"
	triggerWorkflowPath = ".github/workflows/heimdall-trigger.yml"
	// triggerVersion is the one version of the trigger record this build writes and reads.
	triggerVersion = 1
	// maxTriggerBytes bounds the trigger record. It is written by an unprivileged run, so it is untrusted.
	maxTriggerBytes = 1024
	// maxEventBytes bounds the event file GitHub writes. Real payloads are tens of kilobytes.
	maxEventBytes = 25 << 20
	// maxPRNumber keeps a number inside what a change id can hold.
	maxPRNumber = 999_999_999
)

// errMergeGroupNotBuilt is the trigger's answer to a merge_group event: the gate of AM14 judges one pull request, not a
// group commit, so the merge queue half of ACTION_MODE 5.1 is not built. A group with no check stays blocked.
var errMergeGroupNotBuilt = errors.New("merge_group is not built: the gate judges one pull request, not a merge queue group")

// triggerRecord is the small artifact the trigger and proof workflows upload. It exists because workflow_run names no
// pull request for a fork run (ACTION_MODE 3.2). It is untrusted: the gate reads the pull request again from GitHub,
// and accepts a proof artifact only when this record, the run and the pull request agree on the head.
type triggerRecord struct {
	Version int    `json:"version"`
	Event   string `json:"event"`
	Number  int    `json:"number"`
	Head    id.SHA `json:"head"`
}

func (r triggerRecord) check() error {
	switch {
	case r.Version != triggerVersion:
		return fmt.Errorf("trigger record: version %d, want %d", r.Version, triggerVersion)
	case r.Event != "pull_request" && r.Event != "pull_request_review":
		return fmt.Errorf("trigger record: event %q is not a pull request event", clipText(r.Event, 40))
	case r.Number < 1 || r.Number > maxPRNumber:
		return fmt.Errorf("trigger record: number %d is out of range", r.Number)
	}
	if _, err := id.ParseSHA(string(r.Head)); err != nil {
		return fmt.Errorf("trigger record: head: %w", err)
	}
	return nil
}

// prPayload is the part of a pull_request or pull_request_review event the steps read.
type prPayload struct {
	Number      int `json:"number"`
	PullRequest *struct {
		Number int `json:"number"`
		User   struct {
			Login string `json:"login"`
		} `json:"user"`
		Head struct {
			SHA  string `json:"sha"`
			Repo *struct {
				FullName string `json:"full_name"`
			} `json:"repo"`
		} `json:"head"`
	} `json:"pull_request"`
}

// decodeOne reads exactly one JSON value of at most limit bytes into v.
func decodeOne(raw []byte, limit int, strict bool, v any) error {
	if len(raw) > limit {
		return fmt.Errorf("%d bytes is over the %d byte limit", len(raw), limit)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	if strict {
		dec.DisallowUnknownFields()
	}
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("decode: %w", err)
	}
	var extra json.RawMessage
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("it holds more than one JSON value")
	}
	return nil
}

// triggerFromEvent builds the trigger record of a pull_request or pull_request_review event.
func triggerFromEvent(name string, raw []byte) (triggerRecord, error) {
	switch name {
	case "pull_request", "pull_request_review":
	case "merge_group":
		return triggerRecord{}, errMergeGroupNotBuilt
	default:
		return triggerRecord{}, fmt.Errorf("trigger: event %q is not one the trigger workflow runs on", clipText(name, 40))
	}
	var ev prPayload
	if err := decodeOne(raw, maxEventBytes, false, &ev); err != nil {
		return triggerRecord{}, fmt.Errorf("trigger: event: %w", err)
	}
	if ev.PullRequest == nil {
		return triggerRecord{}, errors.New("trigger: the event names no pull request")
	}
	rec := triggerRecord{Version: triggerVersion, Event: name, Number: ev.PullRequest.Number, Head: id.SHA(ev.PullRequest.Head.SHA)}
	if err := rec.check(); err != nil {
		return triggerRecord{}, fmt.Errorf("trigger: %w", err)
	}
	return rec, nil
}

func encodeTriggerRecord(r triggerRecord) ([]byte, error) {
	if err := r.check(); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(r)
	if err != nil {
		return nil, fmt.Errorf("encode trigger record: %w", err)
	}
	return raw, nil
}

// parseTriggerRecord reads a record an unprivileged run uploaded. Unknown fields, another version and a bad value are
// refused.
func parseTriggerRecord(raw []byte) (triggerRecord, error) {
	var r triggerRecord
	if err := decodeOne(raw, maxTriggerBytes, true, &r); err != nil {
		return triggerRecord{}, fmt.Errorf("trigger record: %w", err)
	}
	if err := r.check(); err != nil {
		return triggerRecord{}, err
	}
	return r, nil
}

// workflowRun is the part of a workflow_run event the gate reads. GitHub writes it, so it is trusted, unlike the
// artifacts of the run it names.
type workflowRun struct {
	ID         int64
	Path       string
	Event      string
	HeadSHA    id.SHA
	Conclusion string
	// PRs are the pull requests GitHub lists for the run. The list is empty for a fork run.
	PRs []int
}

// gateEvent is what started a gate run: a finished workflow run, a comment on a pull request, or the sweep.
type gateEvent struct {
	Name string
	Run  *workflowRun
	// Number is the pull request a comment is on, or 0 for a comment on an issue.
	Number int
	Sweep  bool
}

type gatePayload struct {
	WorkflowRun *struct {
		ID           int64  `json:"id"`
		Path         string `json:"path"`
		Event        string `json:"event"`
		HeadSHA      string `json:"head_sha"`
		Conclusion   string `json:"conclusion"`
		PullRequests []struct {
			Number int `json:"number"`
		} `json:"pull_requests"`
	} `json:"workflow_run"`
	Issue *struct {
		Number      int             `json:"number"`
		PullRequest json.RawMessage `json:"pull_request"`
	} `json:"issue"`
}

// parseGateEvent reads the event of a gate run. Only the three triggers of heimdall-gate.yml and a manual dispatch are
// accepted; pull_request_target never is (ACTION_MODE 3.1).
func parseGateEvent(name string, raw []byte) (gateEvent, error) {
	ev := gateEvent{Name: name}
	switch name {
	case "schedule":
		ev.Sweep = true
		return ev, nil
	case "workflow_dispatch":
		return ev, nil
	case "workflow_run", "issue_comment":
	default:
		return gateEvent{}, fmt.Errorf("gate: event %q is not one the gate runs on", clipText(name, 40))
	}
	var p gatePayload
	if err := decodeOne(raw, maxEventBytes, false, &p); err != nil {
		return gateEvent{}, fmt.Errorf("gate: event: %w", err)
	}
	if name == "issue_comment" {
		return commentEvent(ev, p)
	}
	return runEvent(ev, p)
}

// commentEvent reads a comment event. A comment on an issue names no pull request, and the gate then does nothing.
func commentEvent(ev gateEvent, p gatePayload) (gateEvent, error) {
	if p.Issue == nil {
		return gateEvent{}, errors.New("gate: the comment event names no issue")
	}
	if len(p.Issue.PullRequest) > 0 && string(p.Issue.PullRequest) != "null" {
		ev.Number = p.Issue.Number
	}
	return ev, nil
}

// runEvent reads a workflow_run event.
func runEvent(ev gateEvent, p gatePayload) (gateEvent, error) {
	wr := p.WorkflowRun
	if wr == nil || wr.ID <= 0 {
		return gateEvent{}, errors.New("gate: the workflow_run event names no run")
	}
	run := &workflowRun{ID: wr.ID, Path: wr.Path, Event: wr.Event, HeadSHA: id.SHA(wr.HeadSHA), Conclusion: wr.Conclusion}
	for _, pr := range wr.PullRequests {
		if pr.Number >= 1 && pr.Number <= maxPRNumber {
			run.PRs = append(run.PRs, pr.Number)
		}
	}
	ev.Run = run
	return ev, nil
}

// proofUntrusted says why the gate must not read a proof artifact from run, or "" when it may (ACTION_MODE 2.7). The
// run must be the proof workflow on a pull_request event, GitHub's head of the run must be the head the gate serves,
// and the run's own trigger record must name that head. The check that the proof workflow file is the same at the head
// and the merge base is the heimdall_files clause and the workflow_changed row of the gate.
func proofUntrusted(run workflowRun, rec triggerRecord, head id.SHA) string {
	switch {
	case run.Path != proofWorkflowPath:
		return "the run is not " + proofWorkflowPath
	case run.Event != "pull_request":
		return "the proof run was not started by a pull_request event"
	case run.HeadSHA != head:
		return "the proof run is for another head than the pull request's"
	case rec.Head != head:
		return "the proof run's trigger record names another head"
	}
	return ""
}

// proofEvent is what the proof step reads from its pull_request event.
type proofEvent struct {
	Number int
	Head   id.SHA
	// NoKeyByRule is true for a fork or a Dependabot pull request: GitHub gives such a run no repository secret, so a
	// missing model key is the rule, not a mistake (ACTION_MODE 3.4).
	NoKeyByRule bool
}

// parseProofEvent reads the proof step's event. repo is GITHUB_REPOSITORY.
func parseProofEvent(raw []byte, repo string) (proofEvent, error) {
	var ev prPayload
	if err := decodeOne(raw, maxEventBytes, false, &ev); err != nil {
		return proofEvent{}, fmt.Errorf("proof: event: %w", err)
	}
	if ev.PullRequest == nil {
		return proofEvent{}, errors.New("proof: the event names no pull request")
	}
	pr := ev.PullRequest
	rec := triggerRecord{Version: triggerVersion, Event: "pull_request", Number: pr.Number, Head: id.SHA(pr.Head.SHA)}
	if err := rec.check(); err != nil {
		return proofEvent{}, fmt.Errorf("proof: %w", err)
	}
	fork := pr.Head.Repo == nil || !strings.EqualFold(pr.Head.Repo.FullName, repo)
	dependabot := strings.EqualFold(pr.User.Login, "dependabot[bot]")
	return proofEvent{Number: pr.Number, Head: rec.Head, NoKeyByRule: fork || dependabot}, nil
}

// clipText cuts untrusted text for an error message.
func clipText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
