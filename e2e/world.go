package e2e

import (
	"context"
	"fmt"
	"time"
)

// World is the only interface scenarios use. Actions go through Jira, GitHub and the chat; observations come
// from what those show and, only for debugging output, from the server's read-only dashboard.
type World interface {
	Tracker() TrackerUser
	GitHub() GitHubUser
	Chat() ChatUser
	Server() Observer
	Heimdall() CLI
	Clock() Wait
	// Agents starts, stops and counts the work of the agents. It exists on the VM only.
	Agents() AgentControl
	// Scripted sets how the scripted agents behave. It exists only when the agents are scripted.
	Scripted() ScriptedAgents
	// Faults stops and starts parts of the product, the way a bad day does.
	Faults() Faults
	// Runner runs the committed tests the way a person would on the base branch.
	Runner() TestRunner
	// Clean is a clean container with its own Docker daemon, for local mode.
	Clean() CleanRoom
	// Prereq probes one prerequisite for real and says why it is missing.
	Prereq(ctx context.Context, n Need) Availability
}

// Ticket is what a person writes in the tracker.
type Ticket struct {
	Title       string
	Description string
	AC          []string
	Assignee    string
	Labels      []string
}

// TrackerUser does what a person does in Jira.
type TrackerUser interface {
	CreateTicket(ctx context.Context, t Ticket) (key string, err error)
	Move(ctx context.Context, key, status string) error
	Edit(ctx context.Context, key string, t Ticket) error
}

// PR names a pull request.
type PR struct {
	Repo   string
	Number int
	Head   string // head branch
	SHA    string // head commit
}

// GitHubUser does what a person does on GitHub, as a named login. It always acts with an explicit token per call.
type GitHubUser interface {
	// Push commits a change to a branch as a login and returns the new head commit.
	Push(ctx context.Context, repo, branch, as string, c Change) (sha string, err error)
	// SetHead moves the PR's head branch to the tip of fromBranch (the story's fix) and returns the PR with its new head.
	SetHead(ctx context.Context, pr PR, fromBranch, as string) (PR, error)
	// Comments lists the comments on a PR in order.
	Comments(ctx context.Context, pr PR) ([]PRComment, error)
	// ChangedFiles lists the files a branch changes compared with base.
	ChangedFiles(ctx context.Context, repo, base, branch string) ([]string, error)
	// ReadFile returns a file's text at a ref.
	ReadFile(ctx context.Context, repo, ref, path string) (string, error)
	// Mergeable says whether the merge button is unlocked for the PR.
	Mergeable(ctx context.Context, pr PR) (bool, error)
	OpenPR(ctx context.Context, repo, head, base, title, body, as string) (PR, error)
	Comment(ctx context.Context, pr PR, as, body string) (commentID int64, err error)
	EditComment(ctx context.Context, pr PR, as string, commentID int64, body string) error
	// Review submits a review in the given state (APPROVED, COMMENTED) on a commit. GitHub itself may refuse it,
	// for example a self-approval; that is a *ReviewRefusedError.
	Review(ctx context.Context, pr PR, as, state, sha string) error
	Merge(ctx context.Context, pr PR, as string) error
	Close(ctx context.Context, pr PR) error
	Branches(ctx context.Context, repo string) ([]string, error)
	FileLines(ctx context.Context, repo, ref, path string) (lines int, err error)
	Commits(ctx context.Context, repo, branch string) ([]Commit, error)
}

// Change is one commit: files to write (path to content), paths to delete, and the commit message.
type Change struct {
	Files   map[string]string
	Delete  []string
	Message string
}

// PRComment is a comment on a pull request as a person reads it.
type PRComment struct {
	ID     int64
	Author string // the GitHub login; the product's App appears with a [bot] suffix
	Body   string
}

// ReviewRefusedError says GitHub itself refused a review (for example the author approving their own PR).
type ReviewRefusedError struct{ Reason string }

func (e *ReviewRefusedError) Error() string { return "GitHub refused the review: " + e.Reason }

// Commit is what a person sees in a branch's history.
type Commit struct {
	SHA     string
	Author  string
	Message string
	Files   []string
}

// MessageKind says why a message was posted. Only questions and decisions may address a person.
type MessageKind string

// The kinds of message the product may post.
const (
	KindMilestone MessageKind = "milestone"
	KindQuestion  MessageKind = "question"
	KindDecision  MessageKind = "decision"
	KindProgress  MessageKind = "progress"
	KindHuman     MessageKind = "human"
	KindOther     MessageKind = "other"
)

// Message is one chat message as a person reads it.
type Message struct {
	ID        string // the platform's message id, so a transcript read twice is not counted twice
	Room      string
	From      string
	FromAgent bool
	Text      string
	Kind      MessageKind
	Mentions  []string
	At        time.Time
}

// ChatUser does what a person does in the room.
type ChatUser interface {
	Say(ctx context.Context, room, as, text string) error
	Transcript(ctx context.Context, room string) ([]Message, error)
	Rooms(ctx context.Context) ([]string, error)
	// Members lists the chat names in a room.
	Members(ctx context.Context, room string) ([]string, error)
}

// GateState is the heimdall/gate check as a person reads it on GitHub.
type GateState struct {
	State   string   // success, failure, in_progress
	Summary string   // the sentence(s) shown in the check
	Reasons []Reason // every unmet clause
	Head    string   // the commit it is for
	At      time.Time
}

// Reason is one unmet clause: its code for tests, and the sentence a person reads.
type Reason struct {
	Code string
	Text string
}

// Check is one check run as GitHub shows it.
type Check struct {
	Name   string // heimdall/gate, heimdall/acceptance
	Status string // queued, in_progress, success, failure
}

// Finding is a finding as the summary shows it, with how it stands. Confirmed means the evidence was verified by
// code: a runner-confirmed reproduction, or a memory fact marked confirmed with its source.
type Finding struct {
	FindingView
	Blocking  bool // it holds the gate red
	Confirmed bool
	Resolved  bool // a passing re-run of its evidence closed it
	Awaiting  bool // it went back to the reviewer who filed it
	Upheld    bool // an approver upheld it
}

// Observer shows what the server shows a person.
type Observer interface {
	Gate(ctx context.Context, pr PR) (GateState, error)
	// Checks lists the check runs GitHub shows on the PR's head.
	Checks(ctx context.Context, pr PR) ([]Check, error)
	// Findings lists the findings as the summary on the PR shows them.
	Findings(ctx context.Context, pr PR) ([]Finding, error)
	Dashboard(ctx context.Context, changeKey string) (string, error)
	Health(ctx context.Context) error
	// Spend is the OpenRouter usage so far, in dollars, so a step's cost is the difference.
	Spend(ctx context.Context) (float64, error)
	// Counts is what replaying a poll could change: messages per room and check runs per PR.
	Counts(ctx context.Context) (Counts, error)
	// Replay runs the poller once more over the same data.
	Replay(ctx context.Context) error
}

// Counts is a snapshot of what the product has posted.
type Counts struct {
	Messages map[string]int // by room
	Checks   map[string]int // by "repo#number"
}

// CLI is a person typing at the heimdall command line.
type CLI interface {
	Run(ctx context.Context, args ...string) (CLIResult, error)
	// RunWithEnv is Run with extra environment, for example a config path.
	RunWithEnv(ctx context.Context, env []string, args ...string) (CLIResult, error)
}

// CLIResult is what the person saw on the terminal.
type CLIResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// Wait waits on an observable, never blindly. A slow step fails with the number, not a hang.
type Wait interface {
	// Until polls cond until it reports true or timeout passes. cond returns what it saw, for the failure line.
	Until(ctx context.Context, what string, timeout time.Duration, cond func(ctx context.Context) (done bool, saw string, err error)) error
	// Since is how long ago t was, on the world's clock.
	Since(t time.Time) time.Duration
	// Now is the world's current time.
	Now() time.Time
}

// AgentControl starts, stops and counts the work of agents by name. It acts on the VM.
type AgentControl interface {
	Stop(ctx context.Context, agent string) error
	Start(ctx context.Context, agent string) error
	// Tasks is how many tasks each agent has logged so far, by agent name.
	Tasks(ctx context.Context) (map[string]int, error)
}

// ScriptedAgents tells a scripted agent how to behave in its next task. Behaviours are named in the scripted
// agent's own documentation; the step that needs one names it.
type ScriptedAgents interface {
	Script(ctx context.Context, agent, behaviour string) error
}

// Component is a part of the product that can be stopped.
type Component string

// The parts a bad day can stop.
const (
	ComponentServer   Component = "server"
	ComponentSwitch   Component = "switch"
	ComponentMemory   Component = "memory"
	ComponentRunner   Component = "runner"
	ComponentPostgres Component = "postgres"
)

// StartRefusedError says a component refused to start, with the message a person saw.
type StartRefusedError struct {
	Component Component
	Output    string
}

func (e *StartRefusedError) Error() string {
	return fmt.Sprintf("%s refused to start: %s", e.Component, e.Output)
}

// Faults stops and starts components. Stop kills the component; Start starts it and waits until it answers, or
// returns a *StartRefusedError carrying what it printed. Starting a component that already runs does nothing.
type Faults interface {
	Stop(ctx context.Context, c Component) error
	Start(ctx context.Context, c Component) error
}

// TestRun is one committed test run against a base branch.
type TestRun struct {
	Name      string
	Failed    bool
	Assertion bool // it failed on an assertion, not on a compile error or a panic
}

// TestRunner runs the tests a branch added, with the base branch's code.
type TestRunner interface {
	RunOnBase(ctx context.Context, repo, base, branch string) ([]TestRun, error)
}

// CleanRoom is a clean container, with its own Docker daemon, in which only the README has been followed.
type CleanRoom interface {
	Exec(ctx context.Context, args ...string) (CLIResult, error)
	// WriteFile puts a file in the container's working directory.
	WriteFile(ctx context.Context, path, content string) error
	// Dashboard is the text of the container's dashboard.
	Dashboard(ctx context.Context) (string, error)
}
