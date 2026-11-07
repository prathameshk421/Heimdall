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
	"sync"
	"time"

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

// PackReader loads only the pack owned by the authenticated principal. It must resolve the person's identity and
// verification/decryption keys server-side; tool arguments never name an owner.
type PackReader interface {
	Read(context.Context, Principal, Assignment, string) (string, error)
}

// ErrCardNotFound is returned by Cards.Read for a card the caller does not have.
var ErrCardNotFound = errors.New("card not found")

// Cards serves the caller's own personal context cards. The owner is resolved server-side from the authenticated
// agent and its assignment; an empty file lists the card names.
type Cards interface {
	Read(ctx context.Context, p Principal, a Assignment, file string) (string, error)
}

// PackPublisher stages an owner-visible redacted review and publishes exactly the reviewed content.
type PackPublisher interface {
	Review(context.Context, Principal, string, string, map[string][]byte) (string, error)
	Publish(context.Context, Principal, string, string, string, string, map[string][]byte) (string, error)
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
	Trial(context.Context, Principal, string, id.SHA) (ports.ProfileTrial, error)
	Promote(context.Context, Principal, string) error
}

// Deps are the collaborators of a Server. Every field is required.
type Deps struct {
	Logger        *slog.Logger
	Clock         ports.Clock
	Tokens        Tokens
	Tasks         Tasks
	Reviews       Reviews
	Code          Code
	Blobs         Blobs
	Knowledge     Knowledge
	Packs         PackReader
	Cards         Cards
	PackPublisher PackPublisher
	Files         KnowledgeFiles
	Profiles      Profiles
	// Sandbox runs the fixed static checks of heimdall_run_check through the runner. The tool exists only when it is set.
	Sandbox ports.Sandbox
}

// Server answers MCP requests for the agents' tools.
type Server struct {
	deps  Deps
	tools []tool

	// progressMu guards progressLast, the time of each task's last accepted progress report. It is held in memory
	// per process (TECH_SPEC 6.5), so a restart opens the window again.
	progressMu   sync.Mutex
	progressLast map[id.TaskID]time.Time

	// checkMu guards checkRuns, the number of heimdall_run_check calls each task has made. Every call costs a sandbox
	// job, so a task gets maxChecksPerTask of them (TECH_SPEC 14.1, "counts toward budget").
	checkMu   sync.Mutex
	checkRuns map[id.TaskID]int
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
	s := &Server{deps: d, progressLast: map[id.TaskID]time.Time{}, checkRuns: map[id.TaskID]int{}}
	tools, err := s.buildTools()
	if err != nil {
		return nil, fmt.Errorf("build mcp tools: %w", err)
	}
	s.tools = tools
	return s, nil
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
