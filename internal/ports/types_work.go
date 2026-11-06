package ports

import (
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

// The sandbox jobs. Mutation is Later in the build and is listed so the enum matches the spec table.
const (
	SandboxRedFirst SandboxKind = "red_first"
	SandboxRepro    SandboxKind = "repro"
	SandboxStatic   SandboxKind = "static"
	SandboxContract SandboxKind = "contract"
	SandboxMutation SandboxKind = "mutation"
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
}

// Validate returns every problem with the job.
func (j SandboxJob) Validate() error {
	var v problems
	switch j.Kind {
	case SandboxRedFirst, SandboxRepro, SandboxStatic, SandboxContract, SandboxMutation:
	default:
		v.require("kind", false, "must be red_first, repro, static, contract or mutation")
	}
	v.add("repo", checkRepo(j.Repo))
	v.add("sha", checkSHA(j.SHA))
	v.add("dir", checkRelPath(j.Dir))
	v.require("timeout", j.Timeout > 0 && j.Timeout <= maxSandboxTimeout, "must be more than 0 and at most 10 minutes")
	return v.join()
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
}

// Validate returns every problem with the result.
func (r SandboxResult) Validate() error {
	var v problems
	v.require("passed", r.Passed >= 0, "must not be negative")
	v.require("failed", r.Failed >= 0, "must not be negative")
	v.require("errored", r.Errored >= 0, "must not be negative")
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
