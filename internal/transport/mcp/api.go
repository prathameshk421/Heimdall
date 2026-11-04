package mcp

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/testset"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// Principal is who a token belongs to: one agent in one role (TECH_SPEC 15.4).
type Principal struct {
	Agent id.AgentName
	Role  workflow.Role
}

// Assignment is the one active task of an agent. AssignedAt starts the 60 s ack window (TECH_SPEC 6.2).
type Assignment struct {
	Task       workflow.TaskEnvelope
	AssignedAt time.Time
	Acked      bool
}

// ErrNoTask says the agent has no active task. The tool answers it as a refusal, not a protocol error.
var ErrNoTask = errors.New("no active task")

// HashToken returns the lowercase hex SHA-256 of a token. Stored tokens are hashes, so a lookup is by hash.
func HashToken(token string) string { return "" }

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

// Code reads one file at one commit. The adapter is the GitHub code reader (ports.CodeReader).
type Code interface {
	ReadFile(ctx context.Context, repo change.Repo, sha id.SHA, path string) ([]byte, error)
}

// Blobs reads stored content by its reference. The adapter is the blob store (ports.BlobStore).
type Blobs interface {
	Get(ctx context.Context, ref workflow.BlobRef) (io.ReadCloser, error)
}

// Deps are the collaborators of a Server. Every field is required.
type Deps struct {
	Logger *slog.Logger
	Clock  ports.Clock
	Tokens Tokens
	Tasks  Tasks
	Code   Code
	Blobs  Blobs
}

// Server answers MCP requests for the agents' tools.
type Server struct {
	deps Deps
}

// NewServer validates the dependencies and returns the server. A nil dependency is refused.
func NewServer(d Deps) (*Server, error) { return &Server{deps: d}, nil }

// Handler returns the http.Handler for /mcp: POST only, bearer token required.
func (s *Server) Handler() http.Handler { return http.NotFoundHandler() }

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

// Bridge relays JSON-RPC lines from In to the server and the responses to Out, until In ends or ctx is cancelled.
func Bridge(ctx context.Context, cfg BridgeConfig) error { return nil }
