package change

import (
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
)

// The one table of rows (HARNESS_DESIGN B2). A row says when a piece of work is done and what to do while it is not.
// The reconciler reads the table and starts the work; Missing reads the same table and says what the change waits for,
// so what the product does and what it tells people cannot drift apart. Rows never do I/O: everything they read is in In.

// Flow names which part of a change a row belongs to. A change carries up to two flows; each starts on its own fact.
type Flow string

// The flows. FlowBoth rows start when either flow has started.
const (
	FlowTests  Flow = "tests"
	FlowReview Flow = "review"
	FlowBoth   Flow = "both"
)

// Row is one piece of work of a change.
type Row struct {
	ID    string
	Flow  Flow
	After []string // row ids that must be done first
	// Jobs are the effect kinds Next may start. The server checks at start-up that each one has an executor.
	Jobs []EffectKind
	// Done reports whether the row's work is finished. Next is called only when the flow has started, every After row is
	// done and Done is false.
	Done func(In) bool
	Next func(In) []Action
}

// In is everything a row may read. The reconciler builds it; rows never do I/O.
type In struct {
	Change id.ChangeID
	View   View
	Policy policy.Policy // at the PR's base SHA when there is a PR, else at the branch base
	// PolicyProblem says why the policy at that commit cannot be used (it does not decode, names an unknown version or
	// fails Validate). While it is set no row is open, and Missing names it.
	PolicyProblem string
	Outside       Outside // facts that need I/O; zero before a PR
	Profile       Profile // the repo's promoted profile; zero when none
	Settings      Settings
	Dead          []DeadJob // outbox rows of this change that are dead or in doubt
	Now           time.Time
}

// Outside is what the log cannot say and a row needs from the repository.
type Outside struct {
	// ChangedFiles are the files the pull request changes, which decide the profile checks that apply.
	ChangedFiles []string
	// HeadTestTreeHash is the git tree hash of the change's test directory at the pull request head, read from the
	// repository. It is never derived from events: the pin clause exists to catch a PR that changed the pinned tests.
	HeadTestTreeHash           string
	TestDiffKnown              bool
	TestDiffOutsideKey         []string
	CIRequired                 []string
	ApproversForChangedDomains []string
	PolicyKnown                bool
	PolicyAtBase               policy.Policy
	PolicyAtHead               policy.Policy
	PolicyOwnerApproved        bool
	PolicyVersion              int
}

// Profile is the part of the repository's promoted profile the rows read. The zero value means there is none.
type Profile struct {
	// Promoted says the repository has a promoted profile; TestsDir and Checks are its fields, zero when it has none.
	Promoted bool
	// TestsDir is the folder the Heimdall tests live in, such as tests/heimdall; the ticket's folder is inside it.
	TestsDir string
	Checks   []ProfileCheck
	// Latest is the repository's newest proposal in any state, nil when none was ever proposed (D175).
	Latest *ProfileProposal
}

// ProfileProposal is a proposal as the profile row reads it: its id, its state (proposed, approved or promoted) and
// the outcome of its latest trial ("" before any, "passed" or "failed") with the trial's log.
type ProfileProposal struct {
	ID       string
	State    string
	Trial    string
	TrialLog workflow.BlobRef
}

// ProfileCheck is one check of the profile: it applies when a changed file matches one of its When globs (or always
// when When is empty).
type ProfileCheck struct {
	ID   string
	When []string
}

// Settings are the numbers the rows need from configuration.
type Settings struct {
	Reviewers      []ActorID // the seats used when nobody picks and CODEOWNERS suggests none
	TestAttempts   int
	PickWait       time.Duration
	BranchTemplate string // empty means DefaultBranchTemplate
	Timeouts       map[string]time.Duration
	Profiler       string // the agent that holds profile tasks; empty when none is configured (D175)
}

// DeadJob is an outbox row of the change that failed for good or is in doubt. Row is the row that made it.
type DeadJob struct {
	Key       EffectKey
	Kind      EffectKind
	Attempts  int
	LastError string
	InDoubt   bool
}

// Action is exactly one of Job, Fact, Task, Ask. Text is the need in plain words; it is empty only when another part of
// Missing (a gate clause) already says it.
type Action struct {
	Job  *Job
	Fact Payload // an event the server decides from In alone; the reconciler appends it
	Task *Task
	Ask  *Ask
	Text string
}

// Job is an outside effect. The key names its input, so one input runs once and a new input runs again; the payload
// holds only values read from In.
type Job struct {
	Kind    EffectKind
	Key     EffectKey
	Target  string
	Payload any
}

// Task is work for an agent. Kind is a workflow kind name; Subject is what it is about (a seat or a case) and Input
// the input it is made for (a head, an attempt), so a changed input replaces the task.
type Task struct {
	Kind    string
	Subject string
	Input   string
}

// Ask is a question for people.
type Ask struct {
	Who  []ActorID
	What string
}

// started reports whether the row's flow has started on this view.
func (r Row) started(v View) bool {
	switch r.Flow {
	case FlowTests:
		return v.Trigger != nil
	case FlowReview:
		return v.PR != nil
	case FlowBoth:
		return v.Trigger != nil || v.PR != nil
	}
	return false
}

// Rows is the table in order: the order Missing reads and the order the board shows.
func Rows() []Row {
	out := []Row{rowOpenRoom()}
	out = append(out, testsRows()...)
	return append(out, reviewRows()...)
}

// RowJobKinds are the effect kinds the rows may start, each once, in table order.
func RowJobKinds() []EffectKind {
	var out []EffectKind
	for _, r := range Rows() {
		for _, k := range r.Jobs {
			if !slices.Contains(out, k) {
				out = append(out, k)
			}
		}
	}
	return out
}

// Open returns the rows that have work to do: the flow started, every After row is done and the row is not done itself.
// A row of a flow that has not started is neither open nor done, so Done never reads a fact that cannot exist yet.
func Open(in In) []Row {
	if in.PolicyProblem != "" {
		return nil // no row decides on a policy the server cannot read
	}
	rows := Rows()
	done := make(map[string]bool, len(rows))
	for _, r := range rows {
		done[r.ID] = r.started(in.View) && r.Done(in)
	}
	var open []Row
	for _, r := range rows {
		if in.View.Over() || done[r.ID] || !r.started(in.View) || !allDone(done, r.After) {
			continue
		}
		open = append(open, r)
	}
	return open
}

// Next is the actions of every open row, in table order, with the row id each came from.
func Next(in In) []RowAction {
	var out []RowAction
	for _, r := range Open(in) {
		for _, a := range r.Next(in) {
			if a.Task != nil {
				a = capTask(in, r.ID, a)
			}
			out = append(out, RowAction{Row: r.ID, Action: a})
		}
	}
	return out
}

// MaxTaskFailures is how many tasks of one kind and subject may fail or time out on the same input, in one round of
// the row, before the seat is left to a person. Every task costs model tokens; a seat that always fails would loop.
const MaxTaskFailures = 2

// TaskRow is the row that assigns tasks of a kind; `heimdall resume <change> <row>` starts a new round of it.
func TaskRow(kind string) string {
	switch kind {
	case "tests":
		return "write_tests"
	case "review":
		return "review"
	case "verdict":
		return "deliberate"
	}
	return ""
}

// capTask turns a task that already failed MaxTaskFailures times on its input in this round into a question for the
// operators, so the reconciler assigns nothing and Missing says what a person can do.
func capTask(in In, row string, a Action) Action {
	t := a.Task
	failed := 0
	for _, info := range in.View.Tasks {
		if info.Kind == t.Kind && info.Subject == t.Subject && info.Input == t.Input &&
			info.Round == in.View.Resumes[row] && (info.Status == TaskFailedStatus || info.Status == TaskTimedOutStatus) {
			failed++
		}
	}
	if failed < MaxTaskFailures {
		return a
	}
	text := fmt.Sprintf("%s failed %d %s tasks on %s; `heimdall resume %s %s` tries again",
		t.Subject, failed, t.Kind, inputLabel(t.Input), in.Change, row)
	return Action{Ask: &Ask{Who: operators(in), What: text}, Text: text}
}

// inputLabel is a task input in the words people read: a short head for a review, the input itself otherwise.
func inputLabel(input string) string {
	if head, ok := strings.CutPrefix(input, "head-"); ok {
		return short(head)
	}
	return input
}

// RowAction is an action and the row that decided it.
type RowAction struct {
	Row string
	Action
}

// say is an action that is only words: the row waits for something outside the product (a person, a push).
func say(text string) Action { return Action{Text: text} }

func (in In) resumeSuffix(row string) string {
	return "/r" + strconv.Itoa(in.View.Resumes[row])
}

// jobKey is the key of a job: change, the row's own suffix, and the resume count of the row.
func (in In) jobKey(row, suffix string) EffectKey {
	part := row
	if row == "open_room" {
		part = "room"
	}
	k := string(in.Change) + "/" + part
	if suffix != "" {
		k += "/" + suffix
	}
	return EffectKey(k + in.resumeSuffix(row))
}

// TypeOf returns the event type a payload is recorded as.
func TypeOf(p Payload) (EventType, bool) {
	for t, e := range registry() {
		if reflect.TypeOf(e.zero) == reflect.TypeOf(p) {
			return t, true
		}
	}
	return "", false
}

func allDone(done map[string]bool, ids []string) bool {
	for _, id := range ids {
		if !done[id] {
			return false
		}
	}
	return true
}
