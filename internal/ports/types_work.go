package ports

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
)

// AgentRef names an agent a task is sent to.
type AgentRef struct {
	Name id.AgentName  `json:"name"`
	Role workflow.Role `json:"role"`
}

// Validate returns every problem with the reference.
func (a AgentRef) Validate() error {
	var v problems
	v.add("name", checkAgentName(a.Name))
	v.add("role", checkRole(a.Role))
	return v.join()
}

// WakeResult says whether the runtime accepted a wake-up. A refusal is not an error: the caller records it.
type WakeResult struct {
	Accepted bool   `json:"accepted"`
	Reason   string `json:"reason,omitempty"`
}

// Liveness is whether an agent can take work now (TECH_SPEC 6.9).
type Liveness string

// The liveness states.
const (
	LivenessLive    Liveness = "live"
	LivenessDormant Liveness = "dormant"
	LivenessOffline Liveness = "offline"
)

// Valid reports whether l is one of the liveness states.
func (l Liveness) Valid() bool {
	return l == LivenessLive || l == LivenessDormant || l == LivenessOffline
}

// SandboxKind is one of the sandbox jobs of TECH_SPEC 12.2.
type SandboxKind string

// The sandbox jobs. Check and Test are the one job shape of REVIEW_ARCH 4.1 and TEST_AGENT_ARCH 5.1: the head tree
// at /work, an optional base tree at /base, a command taken from a promoted profile, a result file at /out.
const (
	SandboxRedFirst SandboxKind = "red_first"
	SandboxRepro    SandboxKind = "repro"
	// SandboxCheck runs one profile check (CheckID) and returns its SARIF result.
	SandboxCheck SandboxKind = "check"
	// SandboxTest runs one profile test kind (TestKind) over Dir and returns its JUnit result.
	SandboxTest         SandboxKind = "test"
	SandboxProfileTrial SandboxKind = "profile_trial"
	// SandboxWarm is the read-only module-cache fetch job (TECH_SPEC 12.2, D153). It never runs
	// repository code and its egress is allowlisted by the runner's fetch boundary.
	SandboxWarm SandboxKind = "warm"
)

// maxSandboxTimeout is the longest any job may run (TECH_SPEC 12.2: 600 s for mutation, the largest limit).
const maxSandboxTimeout = 10 * time.Minute

// SandboxJob asks the runner for one job at one commit of one repository.
type SandboxJob struct {
	Kind    SandboxKind   `json:"kind"`
	Repo    change.Repo   `json:"repo"`
	SHA     id.SHA        `json:"sha"`
	Dir     string        `json:"dir"`
	Timeout time.Duration `json:"timeout"`
	Profile string        `json:"profile,omitempty"`
	// BaseSHA, when set on a check or test job, is the commit whose tree the job also sees, read-only, at /base.
	BaseSHA id.SHA `json:"base_sha,omitempty"`
	// CheckID names the profile check a check job runs; empty for every other kind.
	CheckID string `json:"check_id,omitempty"`
	// TestKind names the profile test kind a test job runs; empty for every other kind.
	TestKind string `json:"test_kind,omitempty"`
	// OnDemand marks a check job a person or agent asked for (run_check) rather than one the pipeline ran by itself.
	OnDemand bool `json:"on_demand,omitempty"`
	// Verify marks a check job that verifies the evidence of a filed finding (E2). The gate waits on its answer, so it
	// waits in the gate lane, not behind the pipeline's own checks.
	Verify bool `json:"verify,omitempty"`
	// Change is the ticket or pull request this job serves. Only a job with a Change can be superseded by a new head.
	Change id.ChangeID `json:"change,omitempty"`
	// Head is the pull-request head this job serves, when it differs from SHA (a baseline run of a check at the merge
	// base serves the head it will be compared with). Empty means SHA is the head.
	Head id.SHA `json:"head,omitempty"`
}

// Priority orders the runner's queue (TEST_AGENT_ARCH 8): a lower value runs first. It is data on the job kind.
type Priority int

// The priorities, highest first. A preview is the lowest: it is a running process a person is waiting on only
// while an agent explores, never a gate.
const (
	PriorityGate       Priority = iota // acceptance, red-first, evidence verification (E2 checks), profile trials
	PriorityOnDemand                   // a check an agent or person asked for, a cache warm
	PriorityBackground                 // checks and mutants the pipeline runs by itself
	PriorityPreview
)

// Priority returns where the job waits in the runner's queue.
func (j SandboxJob) Priority() Priority {
	switch j.Kind {
	case SandboxRedFirst, SandboxRepro, SandboxTest, SandboxProfileTrial:
		return PriorityGate
	case SandboxWarm:
		return PriorityOnDemand
	case SandboxCheck:
		if j.Verify {
			return PriorityGate
		}
		if j.OnDemand {
			return PriorityOnDemand
		}
	}
	return PriorityBackground
}

// HeadSHA returns the pull-request head the job serves.
func (j SandboxJob) HeadSHA() id.SHA {
	if j.Head != "" {
		return j.Head
	}
	return j.SHA
}

// MaxCheckOutput bounds the text output a job returns.
const MaxCheckOutput = 64 << 10

// MaxJobResult bounds the result file (SARIF or JUnit) a check or test job returns.
const MaxJobResult = 2 << 20

// Validate returns every problem with the job.
func (j SandboxJob) Validate() error {
	var v problems
	switch j.Kind {
	case SandboxRedFirst, SandboxRepro, SandboxProfileTrial, SandboxWarm, SandboxCheck, SandboxTest:
	default:
		v.require("kind", false, "must be a known sandbox job kind")
	}
	v.add("repo", checkRepo(j.Repo))
	v.add("sha", checkSHA(j.SHA))
	v.add("dir", j.validateDir())
	v.require("timeout", j.Timeout > 0 && j.Timeout <= maxSandboxTimeout, "must be more than 0 and at most 10 minutes")
	v.add("base_sha", j.validateBase())
	if j.Head != "" {
		v.add("head", checkSHA(j.Head))
	}
	if j.Change != "" {
		v.add("change", func() error { _, err := id.ParseChangeID(string(j.Change)); return err }())
	}
	v.require("on_demand", !j.OnDemand || j.Kind == SandboxCheck, "only a check job is asked for on demand")
	v.require("verify", !j.Verify || j.Kind == SandboxCheck, "only a check job verifies evidence")
	v.require("verify", !j.Verify || !j.OnDemand, "a verification is not on demand")
	v.add("check_id", j.validateName(j.CheckID, SandboxCheck))
	v.add("test_kind", j.validateName(j.TestKind, SandboxTest))
	return v.join()
}

// validateDir requires a directory for the jobs that run a test command over one and refuses a malformed one for
// every job that names it.
func (j SandboxJob) validateDir() error {
	if (j.Kind == SandboxCheck || j.Kind == SandboxWarm) && j.Dir == "" {
		return nil
	}
	return checkRelPath(j.Dir)
}

func (j SandboxJob) validateBase() error {
	if j.BaseSHA == "" {
		return nil
	}
	if j.Kind != SandboxCheck && j.Kind != SandboxTest {
		return errors.New("only a check or test job sees a base tree")
	}
	return checkSHA(j.BaseSHA)
}

// validateName requires value to be a profile-style name exactly when the job is of kind want.
func (j SandboxJob) validateName(value string, want SandboxKind) error {
	if j.Kind != want {
		if value != "" {
			return fmt.Errorf("only a %s job sets it", want)
		}
		return nil
	}
	if !profileNameRef.MatchString(value) {
		return errors.New("must be one to 64 lowercase name characters")
	}
	return nil
}

// Outcome is what one test did in a sandbox run. Failure is an assertion (JUnit <failure>); Error is a crash, a
// compile error or an import error (JUnit <error>), which never counts as red-first (TECH_SPEC 12.1, D39).
type Outcome string

// The test outcomes.
const (
	OutcomePassed  Outcome = "passed"
	OutcomeFailure Outcome = "failure"
	OutcomeError   Outcome = "error"
	OutcomeSkipped Outcome = "skipped"
)

// TestResult is the parsed outcome of one test.
type TestResult struct {
	Name    string  `json:"name"`
	Outcome Outcome `json:"outcome"`
}

// SandboxResult is the parsed JUnit summary and the blob of the full log (TECH_SPEC 12.2).
type SandboxResult struct {
	Passed  int              `json:"passed"`
	Failed  int              `json:"failed"`
	Errored int              `json:"errored"`
	Tests   []TestResult     `json:"tests"`
	LogRef  workflow.BlobRef `json:"log_ref"`
	// Output is the bounded text a static check printed. It is empty for test jobs.
	Output string `json:"output,omitempty"`
	// Sarif is the result file of a check job, at most MaxJobResult bytes. Its content is untrusted.
	Sarif []byte `json:"sarif,omitempty"`
	// Exit is the exit status of the command of a check or test job.
	Exit int `json:"exit,omitempty"`
}

// Validate returns every problem with the result.
func (r SandboxResult) Validate() error {
	var v problems
	v.require("passed", r.Passed >= 0, "must not be negative")
	v.require("failed", r.Failed >= 0, "must not be negative")
	v.require("errored", r.Errored >= 0, "must not be negative")
	v.require("output", len(r.Output) <= MaxCheckOutput, "must be at most 64 KiB")
	v.require("sarif", len(r.Sarif) <= MaxJobResult, "must be at most 2 MiB")
	for i, t := range r.Tests {
		v.add("tests["+strconv.Itoa(i)+"].name", checkText(t.Name, 512))
		switch t.Outcome {
		case OutcomePassed, OutcomeFailure, OutcomeError, OutcomeSkipped:
		default:
			v.require("tests["+strconv.Itoa(i)+"].outcome", false, "must be passed, failure, error or skipped")
		}
	}
	v.add("log_ref", checkBlob(r.LogRef))
	return v.join()
}

// EpisodeStatus says how far a memory episode can be trusted (TECH_SPEC 9.3).
type EpisodeStatus string

// The episode statuses.
const (
	EpisodeConfirmed EpisodeStatus = "confirmed"
	EpisodeObserved  EpisodeStatus = "observed"
)

// Episode is one thing to remember. GroupID is the team scope; the server sets it, not the caller.
type Episode struct {
	Key     change.EffectKey `json:"key"`
	Source  string           `json:"source"`
	Status  EpisodeStatus    `json:"status"`
	GroupID string           `json:"group_id"`
	Body    string           `json:"body"`
}

// Validate returns every problem with the episode.
func (e Episode) Validate() error {
	var v problems
	v.add("key", checkText(string(e.Key), 256))
	v.add("source", checkText(e.Source, 64))
	switch e.Status {
	case EpisodeConfirmed, EpisodeObserved:
	default:
		v.require("status", false, "must be confirmed or observed")
	}
	v.require("group_id", e.GroupID != "", "must be set by the server")
	v.require("body", len(e.Body) <= MaxUntrustedText, "is too long")
	return v.join()
}

// MemoryQuery asks for facts. Groups are resolved by the server from the caller (TECH_SPEC 9.4); a query without them
// is refused, so no caller can widen its own scope.
type MemoryQuery struct {
	Question string    `json:"question"`
	Entities []string  `json:"entities,omitempty"`
	AsOf     time.Time `json:"as_of,omitempty"`
	Groups   []string  `json:"groups"`
	Limit    int       `json:"limit"`
}

// maxMemoryLimit is the item limit of TECH_SPEC 9.4.
const maxMemoryLimit = 20

// Validate returns every problem with the query.
func (q MemoryQuery) Validate() error {
	var v problems
	v.add("question", checkText(q.Question, 1024))
	v.require("groups", len(q.Groups) > 0, "must be set by the server")
	v.require("limit", q.Limit >= 1 && q.Limit <= maxMemoryLimit, "must be 1 to 20")
	return v.join()
}

// MemoryLabel is the trust label of a returned fact (TECH_SPEC 9.4).
type MemoryLabel string

// The memory labels.
const (
	LabelConfirmed  MemoryLabel = "confirmed"
	LabelSuperseded MemoryLabel = "superseded"
	LabelContested  MemoryLabel = "contested"
	LabelObserved   MemoryLabel = "observed"
)

// Valid reports whether l is a memory label.
func (l MemoryLabel) Valid() bool {
	switch l {
	case LabelConfirmed, LabelSuperseded, LabelContested, LabelObserved:
		return true
	}
	return false
}

// MemoryItem is one fact returned by a search. Only confirmed, non-superseded items are E3 evidence.
type MemoryItem struct {
	Ref       string      `json:"ref"`
	SourceKey string      `json:"source_key,omitempty"`
	Text      string      `json:"text"`
	Label     MemoryLabel `json:"label"`
	SourceURL string      `json:"source_url"`
	ValidAt   time.Time   `json:"valid_at"`
	InvalidAt *time.Time  `json:"invalid_at,omitempty"`
}

// KnowledgeFile records one safe, human-reviewed repository document and its retention metadata.
type KnowledgeFile struct {
	Ref        string
	Repository change.Repo
	Path       string
	SHA        id.SHA
	Group      string
	SourceURL  string
	Blob       workflow.BlobRef
	Kind       string
	State      string
	CreatedAt  time.Time
	ExpiresAt  time.Time
	ApprovedBy string
}

// Validate returns every problem with the item.
func (m MemoryItem) Validate() error {
	var v problems
	v.add("ref", checkText(m.Ref, 256))
	v.require("label", m.Label.Valid(), "must be confirmed, superseded, contested or observed")
	return v.join()
}
