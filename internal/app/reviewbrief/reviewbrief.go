package reviewbrief

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/rulecard"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// Default section deadlines. They keep get_task fast when a source hangs.
const (
	defaultGraphWait    = 20 * time.Second
	defaultLearningWait = 8 * time.Second
	defaultMemoryWait   = 8 * time.Second
)

// Brief holds the sections of one reviewer's brief. Each text may be empty.
type Brief struct {
	Obligations   string
	ChangeMap     string
	PriorFindings string
	Memory        string
	Record        string
	// ObligationCards lists the file names of the cards that Obligations shows, in the same order.
	ObligationCards []string
	// Partial says that a section failed, so a caller should compile again next time and not reuse this brief.
	Partial bool
}

// Request names the change and the reviewer for one brief.
type Request struct {
	// Person owns the cards. It is empty when the server could not resolve the person.
	Person string
	Agent  id.AgentName
	Task   workflow.TaskEnvelope
	// Diff is the diff text. It may be cut or empty.
	Diff string
}

// Cards returns every card of a person.
type Cards interface {
	All(person string) ([]rulecard.Card, error)
}

// Impact is the view of the code graph result that a brief needs.
type Impact interface {
	Render(maxBytes int) string
	Symbols() []string
	DependentFiles() []string
	// Defined reports whether a symbol has a definition at head.
	Defined(symbol string) bool
	// Partial reports that the head index is truncated, so an absent definition proves nothing.
	Partial() bool
	// BaseMissing reports that the base index was not ready, so removed symbols are not listed.
	BaseMissing() bool
}

// Graph computes the impact of a change at its head and base commits.
type Graph interface {
	Impact(ctx context.Context, repo change.Repo, head, base id.SHA, diff string, files []string) (Impact, error)
}

// Calibration is how one reviewer's findings have fared.
type Calibration struct {
	Useful, Noise, Ignored int
	NoisyCategories        []string
	ResolutionRate         float64
}

// PriorFinding is a finding on the same files from another change, with its outcome.
type PriorFinding struct {
	Change                              id.ChangeID
	Ref, File, Claim, Category, Outcome string
	Reason                              string
}

// History is one read of what past reviews turned into.
type History interface {
	CardRecords(agent id.AgentName) map[string]rulecard.Record
	Calibration(agent id.AgentName) Calibration
	PriorFindings(repo change.Repo, files []string, exclude id.ChangeID, limit int) []PriorFinding
}

// Learning returns the current history.
type Learning interface {
	Snapshot(ctx context.Context) (History, error)
}

// Memory searches team memory for one task. It has the same shape as the MCP knowledge boundary, which sets the groups.
type Memory interface {
	Ask(ctx context.Context, agent id.AgentName, task id.TaskID, repo change.Repo, base id.SHA, query ports.MemoryQuery) ([]ports.MemoryItem, error)
}

// Config holds the sources of a Compiler. Any source may be nil, which turns its section off. A zero wait uses the
// default.
type Config struct {
	Logger       *slog.Logger
	Cards        Cards
	Graph        Graph
	Learning     Learning
	Memory       Memory
	GraphWait    time.Duration
	LearningWait time.Duration
	MemoryWait   time.Duration
}

// Compiler builds briefs.
type Compiler struct {
	cfg Config
}

// New returns a Compiler. Only the logger is required.
func New(cfg Config) (*Compiler, error) {
	if cfg.Logger == nil {
		return nil, errors.New("reviewbrief logger must not be nil")
	}
	if cfg.GraphWait <= 0 {
		cfg.GraphWait = defaultGraphWait
	}
	if cfg.LearningWait <= 0 {
		cfg.LearningWait = defaultLearningWait
	}
	if cfg.MemoryWait <= 0 {
		cfg.MemoryWait = defaultMemoryWait
	}
	return &Compiler{cfg: cfg}, nil
}

// sources holds what the slow sources returned for one compile. A nil value means the source is off or failed.
type sources struct {
	impact      Impact
	graphFailed bool
	history     History
	learnFailed bool
}

// Compile builds the brief for one task. Each section fails alone, so it never returns an error.
func (c *Compiler) Compile(ctx context.Context, req Request) Brief {
	src := c.gather(ctx, req)
	b := Brief{Partial: src.graphFailed || src.learnFailed || (src.impact != nil && src.impact.BaseMissing())}
	b.ChangeMap = changeMapSection(src)
	b.PriorFindings = priorSection(src, req)
	if req.Task.Role != workflow.RoleReviewer {
		return b
	}
	var cardsOK, memoryOK bool
	b.Obligations, b.ObligationCards, cardsOK = c.obligations(ctx, req, src)
	b.Memory, memoryOK = c.memory(ctx, req, src.impact)
	b.Record = recordSection(src.history, req.Agent)
	b.Partial = b.Partial || !cardsOK || !memoryOK
	return b
}

// gather reads the graph and the history at the same time, so the slow one does not delay the other.
func (c *Compiler) gather(ctx context.Context, req Request) sources {
	var src sources
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		src.impact, src.graphFailed = c.readGraph(ctx, req)
	}()
	go func() {
		defer wg.Done()
		src.history, src.learnFailed = c.readHistory(ctx, req)
	}()
	wg.Wait()
	return src
}

func (c *Compiler) readGraph(ctx context.Context, req Request) (Impact, bool) {
	if c.cfg.Graph == nil {
		return nil, false
	}
	ctx, cancel := context.WithTimeout(ctx, c.cfg.GraphWait)
	defer cancel()
	ref := req.Task.Change
	impact, err := c.cfg.Graph.Impact(ctx, change.Repo(ref.Repo), ref.HeadSHA, ref.BaseSHA, req.Diff, req.Task.Inputs.FilesChanged)
	if err != nil {
		c.warn(ctx, "code graph unavailable for brief", req, err)
		return nil, true
	}
	return impact, false
}

func (c *Compiler) readHistory(ctx context.Context, req Request) (History, bool) {
	if c.cfg.Learning == nil {
		return nil, false
	}
	ctx, cancel := context.WithTimeout(ctx, c.cfg.LearningWait)
	defer cancel()
	history, err := c.cfg.Learning.Snapshot(ctx)
	if err != nil {
		c.warn(ctx, "review history unavailable for brief", req, err)
		return nil, true
	}
	return history, false
}

func (c *Compiler) warn(ctx context.Context, msg string, req Request, err error) {
	c.cfg.Logger.WarnContext(ctx, msg, "agent", string(req.Agent), "task", string(req.Task.TaskID), "error", err)
}
