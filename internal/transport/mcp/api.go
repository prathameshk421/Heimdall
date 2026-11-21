package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/codegraph"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/reviewbrief"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/deliberation"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/testset"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// Principal is who a token belongs to: one agent in one role (TECH_SPEC 15.4).
type Principal struct {
	Agent  id.AgentName
	Role   workflow.Role
	Person string
	Human  bool
	// Ticket is the ticket the call names, when the agent passed one. Empty means the agent's oldest open task.
	Ticket id.ChangeID
}

// Assignment is the one active task of an agent. AssignedAt starts the 60 s ack window (TECH_SPEC 6.2).
type Assignment struct {
	Task       workflow.TaskEnvelope
	AssignedAt time.Time
	Acked      bool
	History    []string
	// Rejection and Attempt are the test author's: why the last submission was refused, and which attempt this is.
	Rejection []string
	Attempt   int
	// Leads are the profile check results on the head, one line each.
	Leads []string
}

// ErrInvalidSubmission marks a problem the application found in what the agent submitted. Its message names only the
// agent's own input, so the tool shows it to the agent as a refusal; every other failure stays generic.
var ErrInvalidSubmission = errors.New("invalid submission")

// ErrNoTask says the agent has no active task. The tool answers it as a refusal, not a protocol error.
var ErrNoTask = errors.New("no active task")

// HashToken returns the lowercase hex SHA-256 of a token. Stored tokens are hashes, so a lookup is by hash.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// Tokens resolves a token hash to its principal. Unknown or revoked is ports.ErrUnauthorized.
type Tokens interface {
	Resolve(ctx context.Context, tokenHash string) (Principal, error)
}

// Tasks is the application side of an agent's task. The app package satisfies it (T23, T30); until then the tests
// use a fake. Each method acts on the assignment it is given and never on another agent's.
type Tasks interface {
	Current(ctx context.Context, p Principal) (Assignment, error)
	Ack(ctx context.Context, a Assignment, at time.Time) error
	SubmitTests(ctx context.Context, a Assignment, sub testset.Submission, late bool) error
	AskHumans(ctx context.Context, a Assignment, questions []string) error
	Progress(ctx context.Context, a Assignment, text string, at time.Time) error
}

// FindingError reports a validation problem with one finding in a review.
type FindingError struct {
	Index   int    `json:"index"`
	Message string `json:"message"`
}

// SubmitReceipt tells the agent what was accepted and what was returned for fixing.
// If all findings were accepted (or the review is clean), FindingErrors is empty.
type SubmitReceipt struct {
	Accepted      bool           `json:"accepted"`
	FindingErrors []FindingError `json:"finding_errors,omitempty"`
}

// Reviews is the application side of reviews and verdicts. The app package satisfies it (T44, T60); until then the
// tests use a fake.
type Reviews interface {
	SubmitReview(ctx context.Context, a Assignment, r finding.Review, late bool) (SubmitReceipt, error)
	SubmitVerdict(ctx context.Context, a Assignment, v deliberation.Verdict, late bool) error
	// RecordProfileProposed records the profiler's proposal on the change, which ends its task (D175).
	RecordProfileProposed(ctx context.Context, a Assignment, proposalID string, repo change.Repo) error
}

// Code reads one file at one commit. The adapter is the GitHub code reader (ports.CodeReader).
type Code interface {
	ReadFile(ctx context.Context, repo change.Repo, sha id.SHA, path string) ([]byte, error)
}

// Blobs reads stored content by its reference. The adapter is the blob store (ports.BlobStore).
type Blobs interface {
	Get(ctx context.Context, ref workflow.BlobRef) (io.ReadCloser, error)
}

// Knowledge is the app boundary for team-scoped Graphiti searches. Groups are resolved by the app, never by the tool.
type Knowledge interface {
	Ask(ctx context.Context, agent id.AgentName, task id.TaskID, repo change.Repo, base id.SHA, query ports.MemoryQuery) ([]ports.MemoryItem, error)
}

// ErrCardNotFound is returned by Cards.Read for a card the caller does not have.
var ErrCardNotFound = errors.New("card not found")

// Cards serves the caller's own personal context cards. The owner is resolved server-side from the authenticated
// agent and its assignment; an empty file lists the card names.
type Cards interface {
	Read(ctx context.Context, p Principal, a Assignment, file string) (string, error)
	// ForTask returns the text of the caller's cards that apply to the files this task changes, or "" when none do.
	ForTask(ctx context.Context, p Principal, a Assignment) (string, error)
}

// Briefs compiles what a reviewer or a resolver knows before it reads the diff. The person is resolved server-side from
// the authenticated agent and its assignment. A failed section is a note inside the brief, not an error.
type Briefs interface {
	Brief(ctx context.Context, p Principal, a Assignment) (reviewbrief.Brief, error)
}

// CodeGraph returns the code index of one commit. The tools ask only for the head commit of the caller's assignment.
type CodeGraph interface {
	Index(ctx context.Context, repo change.Repo, sha id.SHA) (*codegraph.Index, error)
}

// KnowledgeFileInfo is the non-content metadata returned by safe knowledge-file operations.
type KnowledgeFileInfo struct {
	Ref       string    `json:"ref"`
	Path      string    `json:"path"`
	SHA       string    `json:"sha"`
	Group     string    `json:"group"`
	SourceURL string    `json:"source_url,omitempty"`
	State     string    `json:"state"`
	ExpiresAt time.Time `json:"expires_at"`
	Removed   []string  `json:"removed,omitempty"`
}

// KnowledgeFiles is the application boundary for policy-scoped staging and person-authorized approval/deletion.
type KnowledgeFiles interface {
	Stage(context.Context, Principal, Assignment, string, string, string, string, int) (KnowledgeFileInfo, error)
	Approve(context.Context, Principal, string) error
	Forget(context.Context, Principal, string) error
}

// Profiles is the authenticated sandbox-profile lifecycle available to profile agents and mapped owners.
type Profiles interface {
	Propose(context.Context, Principal, string, []byte) (ports.ProfileProposal, bool, error)
	Approve(context.Context, Principal, string) error
	// PreviousTrial is the failed trial of the repository's newest proposal, empty when there is none (D175).
	PreviousTrial(ctx context.Context, repo string) (string, error)
	Promote(context.Context, Principal, string) error
}

// Deps are the collaborators of a Server. Every field is required.
type Deps struct {
	Logger    *slog.Logger
	Clock     ports.Clock
	Tokens    Tokens
	Tasks     Tasks
	Reviews   Reviews
	Code      Code
	Blobs     Blobs
	Knowledge Knowledge
	Cards     Cards
	Files     KnowledgeFiles
	Profiles  Profiles
	// Sandbox runs the profile checks of run_check through the runner.
	Sandbox ports.Sandbox
	// Checks lists the checks of the repository profile for run_check. The tool exists only when Sandbox and Checks are set.
	Checks Checks
	// Briefs adds the compiled brief to get_task. Without it, get_task falls to Cards.ForTask for personal_context.
	Briefs Briefs
	// Graph backs search_code and find_symbol. The tools exist only when it is set.
	Graph CodeGraph
	// Proposer backs propose_command. The tool exists only when it is set.
	Proposer Proposer
	// Paths lists a repository's files for the repo map in get_task and the root of read_code. Without it, neither.
	Paths ports.RepoPaths
	// Needs says in plain words what a change still needs; get_task shows it. Without it the list is empty.
	Needs func(ctx context.Context, change id.ChangeID) ([]string, error)
	// Calls is the record of tool calls. Without it nothing is recorded and repeats are not detected.
	Calls ports.ToolCalls
	// Withheld names the registered tools that are not offered, each with the reason: the boot self-check found the
	// dependency the tool calls unreachable (HARNESS_DESIGN B8). A withheld tool is not listed and a call to it is
	// answered like a call to an unknown tool. It still counts as registered for the kind-table check.
	Withheld map[string]string
}

// Server answers MCP requests for the agents' tools.
type Server struct {
	deps  Deps
	tools []tool
	// registered are the names of every tool the server built, withheld ones included.
	registered []string

	// progressMu guards progressLast, the time of each task's last accepted progress report. It is held in memory
	// per process (TECH_SPEC 6.5), so a restart opens the window again.
	progressMu   sync.Mutex
	progressLast map[id.TaskID]time.Time

	// checkMu guards checkRuns, the number of heimdall_run_check calls each task has made. Every call costs a sandbox
	// job, so a task gets maxChecksPerTask of them (TECH_SPEC 14.1, "counts toward budget").
	checkMu   sync.Mutex
	checkRuns map[id.TaskID]int

	// briefMu guards briefs, the last brief of each task. A cheap model can call get_task many times, and submit_review
	// reads the obligation names from here. It is held in memory, so a restart only compiles again.
	briefMu sync.Mutex
	briefs  map[id.TaskID]heldBrief
}

// NewServer validates the dependencies and returns the server. A nil dependency is refused.
func NewServer(d Deps) (*Server, error) {
	switch {
	case d.Logger == nil:
		return nil, errors.New("mcp server logger must not be nil")
	case d.Clock == nil:
		return nil, errors.New("mcp server clock must not be nil")
	case d.Tokens == nil:
		return nil, errors.New("mcp server token store must not be nil")
	case d.Tasks == nil:
		return nil, errors.New("mcp server tasks must not be nil")
	case d.Reviews == nil:
		return nil, errors.New("mcp server reviews must not be nil")
	case d.Code == nil:
		return nil, errors.New("mcp server code reader must not be nil")
	case d.Blobs == nil:
		return nil, errors.New("mcp server blob store must not be nil")
	case d.Knowledge == nil:
		return nil, errors.New("mcp server knowledge service must not be nil")
	}
	s := &Server{deps: d, progressLast: map[id.TaskID]time.Time{}, checkRuns: map[id.TaskID]int{}, briefs: map[id.TaskID]heldBrief{}}
	tools, err := s.buildTools()
	if err != nil {
		return nil, fmt.Errorf("build mcp tools: %w", err)
	}
	s.tools, s.registered, err = withhold(tools, d.Withheld)
	if err != nil {
		return nil, err
	}
	return s, nil
}

// withhold removes the withheld tools from the offered ones. A withheld name that is no registered tool is refused: it
// would hide a typo in the wiring.
func withhold(tools []tool, withheld map[string]string) ([]tool, []string, error) {
	registered := make([]string, 0, len(tools))
	offered := make([]tool, 0, len(tools))
	for _, t := range tools {
		registered = append(registered, t.name)
		if _, ok := withheld[t.name]; !ok {
			offered = append(offered, t)
		}
	}
	for name := range withheld {
		if !slices.Contains(registered, name) {
			return nil, nil, fmt.Errorf("mcp server: withheld tool %q is not registered", name)
		}
	}
	return offered, registered, nil
}

// Handler returns the http.Handler for /mcp: POST only. The bearer token is checked inside the handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /mcp", s.serveMCP)
	return mux
}

// BridgeConfig configures the stdio bridge. URL is the server's /mcp endpoint; Token is the agent token and is
// sent only as a header. In and Out are the client's stdio. Logs go to Logger, never to Out.
type BridgeConfig struct {
	URL    string
	Token  string
	In     io.Reader
	Out    io.Writer
	Logger *slog.Logger
	HTTP   *http.Client
}
