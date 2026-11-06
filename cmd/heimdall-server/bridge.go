package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/localrunner"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/agenttools"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/engine"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/gateuse"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/intake"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/prsync"
	appreview "github.com/prathameshk421/flip_the_switch_hackathon/internal/app/review"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/deliberation"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/testset"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/platform/config"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/transport/mcp"
)

// starter opens a change with its first event. It adapts engine.Start, which also returns an outcome the poller
// does not use (trigger.Starter).
type starter struct {
	eng *engine.Engine
}

// Start opens the change. A change that already exists returns an error that wraps ports.ErrVersionConflict.
func (s starter) Start(ctx context.Context, first change.Event) error {
	_, err := s.eng.Start(ctx, first)
	return err
}

// ingestor hands the events of an executor to the engine (effects.Ingest). It adapts engine.Advance, which also
// returns an outcome the executors do not use.
type ingestor struct {
	eng *engine.Engine
}

// Ingest stores the events and runs the steps that follow them.
func (i ingestor) Ingest(ctx context.Context, changeID id.ChangeID, evs []change.Event) error {
	_, err := i.eng.Advance(ctx, changeID, evs)
	return err
}

// cursorTable is the cursor part of postgres.Store.
type cursorTable interface {
	GetCursor(ctx context.Context, source string) (string, error)
	SetCursor(ctx context.Context, source, value string) error
}

// cursorStore keeps the poll cursor of one tracker source in the cursor table (trigger.CursorStore). A source with no
// cursor yet loads as the empty cursor, so the first poll starts one overlap window back.
type cursorStore struct {
	table  cursorTable
	source string
}

// Load returns the saved cursor, or the empty cursor when none is saved.
func (c cursorStore) Load(ctx context.Context) (ports.Cursor, error) {
	value, err := c.table.GetCursor(ctx, c.source)
	if errors.Is(err, ports.ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("load cursor %s: %w", c.source, err)
	}
	return ports.Cursor(value), nil
}

// Save stores the cursor.
func (c cursorStore) Save(ctx context.Context, cur ports.Cursor) error {
	if err := c.table.SetCursor(ctx, c.source, string(cur)); err != nil {
		return fmt.Errorf("save cursor %s: %w", c.source, err)
	}
	return nil
}

// intakeHandler is the part of intake.Intake that prIntake needs.
type intakeHandler interface {
	HandlePR(ctx context.Context, repo change.Repo, n int) (intake.Result, error)
}

// prIntake adapts intake.Intake to prsync.Intake.
type prIntake struct {
	in intakeHandler
}

// HandlePR runs intake for pull request n and returns the change ID it belongs to.
func (p prIntake) HandlePR(ctx context.Context, repo change.Repo, n int) (id.ChangeID, error) {
	res, err := p.in.HandlePR(ctx, repo, n)
	if err != nil {
		return "", err
	}
	return res.Stream, nil
}

// gateEvaluator is the part of gateuse.Evaluator that prGate needs.
type gateEvaluator interface {
	Evaluate(ctx context.Context, changeID id.ChangeID) (gateuse.Result, error)
}

// prGate adapts gateuse.Evaluator to prsync.Gate.
type prGate struct {
	eval gateEvaluator
}

// Evaluate evaluates the merge gate for changeID.
func (g prGate) Evaluate(ctx context.Context, changeID id.ChangeID) error {
	_, err := g.eval.Evaluate(ctx, changeID)
	return err
}

// reviewsAdapter maps ports.ReviewReader to prsync.Reviews.
type reviewsAdapter struct {
	reader ports.ReviewReader
}

// PRReviews returns the submitted reviews of pull request n, mapping ports types to prsync mirror types.
func (r reviewsAdapter) PRReviews(ctx context.Context, repo change.Repo, n int) ([]prsync.PRReview, error) {
	reviews, err := r.reader.PRReviews(ctx, repo, n)
	if err != nil {
		return nil, err
	}
	out := make([]prsync.PRReview, len(reviews))
	for i, rev := range reviews {
		out[i] = prsync.PRReview{
			ID:          rev.ID,
			Login:       rev.Login,
			State:       prsync.ReviewState(rev.State),
			CommitSHA:   rev.CommitSHA,
			SubmittedAt: rev.SubmittedAt,
		}
	}
	return out, nil
}

// checksAdapter maps ports.CheckReader to prsync.Checks.
type checksAdapter struct {
	reader ports.CheckReader
}

// CheckRuns returns the check runs on sha, mapping ports types to prsync mirror types.
func (c checksAdapter) CheckRuns(ctx context.Context, repo change.Repo, sha id.SHA) ([]prsync.CheckRun, error) {
	runs, err := c.reader.CheckRuns(ctx, repo, sha)
	if err != nil {
		return nil, err
	}
	out := make([]prsync.CheckRun, len(runs))
	for i, run := range runs {
		out[i] = prsync.CheckRun{
			ID:         run.ID,
			Name:       run.Name,
			Status:     prsync.CheckStatus(run.Status),
			Conclusion: run.Conclusion,
			SHA:        run.SHA,
		}
	}
	return out, nil
}

// memBlobStore is an in-memory blob store for session blobs and bundles.
type memBlobStore struct {
	mu    sync.RWMutex
	blobs map[workflow.BlobRef][]byte
}

func newMemBlobStore() *memBlobStore {
	return &memBlobStore{blobs: make(map[workflow.BlobRef][]byte)}
}

func (m *memBlobStore) Put(_ context.Context, _ ports.BlobClass, r io.Reader) (workflow.BlobRef, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	ref := workflow.BlobRef("blob:sha256:" + hex.EncodeToString(sum[:]))
	m.mu.Lock()
	defer m.mu.Unlock()
	m.blobs[ref] = data
	return ref, nil
}

func (m *memBlobStore) Get(_ context.Context, ref workflow.BlobRef) (io.ReadCloser, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	data, ok := m.blobs[ref]
	if !ok {
		return nil, ports.ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

// chatAsker adapts ports.Chat to agenttools.Asker.
type chatAsker struct {
	chat  ports.Chat
	store ports.EventStore
	ids   ports.IDs
}

func (c *chatAsker) Ask(ctx context.Context, changeID id.ChangeID, question string) (change.QuestionID, change.SenderID, change.MessageID, error) {
	roomID := change.RoomID(changeID)
	if c.store != nil {
		if evs, err := c.store.Load(ctx, changeID); err == nil {
			for _, e := range evs {
				if e.Type == change.EventRoomOpened {
					if p, err := change.Decode(e); err == nil {
						if ro, ok := p.(change.RoomOpened); ok && ro.RoomID != "" {
							roomID = ro.RoomID
							break
						}
					}
				}
			}
		}
	}
	key := change.EffectKey(fmt.Sprintf("ask:%s:%s", changeID, c.ids.NewEvent()))
	if c.chat != nil {
		if err := c.chat.Post(ctx, roomID, key, question); err != nil {
			return "", "", "", err
		}
	}
	qID := change.QuestionID(c.ids.NewEvent())
	return qID, change.SenderID("heimdall"), change.MessageID(key), nil
}

// tokenStore stores hashed agent tokens and maps them to principals (mcp.Tokens).
type tokenStore struct {
	principals map[string]mcp.Principal
}

func newTokenStore(cfg config.Config) (*tokenStore, error) {
	m := make(map[string]mcp.Principal)
	type agentRole struct {
		agent string
		role  workflow.Role
	}
	var list []agentRole
	if cfg.Agents.TestAuthor != "" {
		list = append(list, agentRole{agent: cfg.Agents.TestAuthor, role: workflow.RoleTestAuthor})
	}
	for _, r := range cfg.Agents.Reviewers {
		if r != "" {
			list = append(list, agentRole{agent: r, role: workflow.RoleReviewer})
		}
	}
	if cfg.Agents.Resolver != "" {
		list = append(list, agentRole{agent: cfg.Agents.Resolver, role: workflow.RoleResolver})
	}

	for _, ar := range list {
		token, err := cfg.Secret("agent_token_" + ar.agent)
		if err != nil {
			return nil, fmt.Errorf("agent_token_%s: %w", ar.agent, err)
		}
		hash := mcp.HashToken(token)
		m[hash] = mcp.Principal{
			Agent: id.AgentName(ar.agent),
			Role:  ar.role,
		}
	}
	return &tokenStore{principals: m}, nil
}

func (s *tokenStore) Resolve(_ context.Context, tokenHash string) (mcp.Principal, error) {
	p, ok := s.principals[tokenHash]
	if !ok {
		return mcp.Principal{}, ports.ErrUnauthorized
	}
	return p, nil
}

// assignmentAgentTracker tracks the agent assigned to each task in memory.
type assignmentAgentTracker struct {
	mu     sync.RWMutex
	agents map[id.TaskID]id.AgentName
}

func (tr *assignmentAgentTracker) record(taskID id.TaskID, agent id.AgentName) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	tr.agents[taskID] = agent
}

func (tr *assignmentAgentTracker) agentFor(taskID id.TaskID) id.AgentName {
	tr.mu.RLock()
	defer tr.mu.RUnlock()
	return tr.agents[taskID]
}

func (tr *assignmentAgentTracker) toAgenttools(a mcp.Assignment) agenttools.Assignment {
	agent := tr.agentFor(a.Task.TaskID)
	return toAgenttoolsAssignment(a, agent)
}

// tasksAdapter adapts agenttools.Service to mcp.Tasks.
type tasksAdapter struct {
	svc          *agenttools.Service
	tracker      *assignmentAgentTracker
	advanceTests func(context.Context, id.ChangeID, change.Event) error
	filter       interface {
		Filter(context.Context, string) (string, error)
	}
}

func (t *tasksAdapter) Current(ctx context.Context, p mcp.Principal) (mcp.Assignment, error) {
	assign, err := t.svc.Current(ctx, p.Agent, p.Role)
	if err != nil {
		if errors.Is(err, agenttools.ErrNoTask) {
			return mcp.Assignment{}, mcp.ErrNoTask
		}
		return mcp.Assignment{}, err
	}
	t.tracker.record(assign.TaskID, p.Agent)
	return toMCPAssignment(assign), nil
}

func (t *tasksAdapter) Ack(ctx context.Context, a mcp.Assignment, at time.Time) error {
	return t.svc.Ack(ctx, t.tracker.toAgenttools(a), at)
}

func (t *tasksAdapter) SubmitTests(ctx context.Context, a mcp.Assignment, sub testset.Submission, late bool) error {
	assignment := t.tracker.toAgenttools(a)
	ev, prepared, isLate, err := t.svc.PrepareTestsSubmission(ctx, assignment, sub, late)
	if err != nil {
		return err
	}
	if !prepared {
		return nil
	}
	if t.advanceTests == nil {
		return errors.New("submit tests: workflow advancer is not configured")
	}
	if err := t.advanceTests(ctx, assignment.ChangeID, ev); err != nil {
		return fmt.Errorf("advance workflow after test submission: %w", err)
	}
	if isLate {
		return fmt.Errorf("%w: test submission received after deadline", agenttools.ErrLateSubmission)
	}
	return nil
}

func (t *tasksAdapter) AskHumans(ctx context.Context, a mcp.Assignment, questions []string) error {
	return t.svc.AskHumans(ctx, t.tracker.toAgenttools(a), questions)
}

func (t *tasksAdapter) Progress(ctx context.Context, a mcp.Assignment, text string, at time.Time) error {
	if t.filter != nil {
		filtered, err := t.filter.Filter(ctx, text)
		if err != nil {
			return err
		}
		text = filtered
	}
	return t.svc.Progress(ctx, t.tracker.toAgenttools(a), text, at)
}

type outputAudit struct{ logger *slog.Logger }

func (a outputAudit) RejectedOutput(ctx context.Context, cause string) {
	a.logger.WarnContext(ctx, "agent output rejected", "reason", cause)
}

var _ appreview.OutputAudit = outputAudit{}

// mcpReviewsAdapter adapts agenttools.Service to mcp.Reviews.
type mcpReviewsAdapter struct {
	svc     *agenttools.Service
	tracker *assignmentAgentTracker
}

func (r *mcpReviewsAdapter) SubmitReview(ctx context.Context, a mcp.Assignment, rev finding.Review, late bool) (mcp.SubmitReceipt, error) {
	rcpt, err := r.svc.SubmitReview(ctx, r.tracker.toAgenttools(a), rev, late)
	if err != nil {
		return mcp.SubmitReceipt{}, err
	}
	return toMCPReceipt(rcpt), nil
}

func (r *mcpReviewsAdapter) SubmitVerdict(ctx context.Context, a mcp.Assignment, v deliberation.Verdict, late bool) error {
	return r.svc.SubmitVerdict(ctx, r.tracker.toAgenttools(a), v, late)
}

func toMCPAssignment(a agenttools.Assignment) mcp.Assignment {
	return mcp.Assignment{
		Task:       a.Task,
		AssignedAt: a.AssignedAt,
		Acked:      a.Acked,
	}
}

func toAgenttoolsAssignment(a mcp.Assignment, agent id.AgentName) agenttools.Assignment {
	return agenttools.Assignment{
		ChangeID:   a.Task.Change.ID,
		TaskID:     a.Task.TaskID,
		Agent:      agent,
		Role:       a.Task.Role,
		Deadline:   a.Task.Deadline,
		AssignedAt: a.AssignedAt,
		Acked:      a.Acked,
		Task:       a.Task,
	}
}

func toMCPReceipt(r agenttools.Receipt) mcp.SubmitReceipt {
	var fes []mcp.FindingError
	for _, fe := range r.FindingErrors {
		fes = append(fes, mcp.FindingError{
			Index:   fe.Index,
			Message: fe.Message,
		})
	}
	return mcp.SubmitReceipt{
		Accepted:      r.Accepted,
		FindingErrors: fes,
	}
}

// vcsSource adapts vcsPort to effects.Source.
// Fetch proves the commit is readable; the sandbox still pulls the tarball when it runs.
type vcsSource struct {
	vcs vcsPort
}

func (s vcsSource) Fetch(ctx context.Context, repo change.Repo, sha id.SHA) error {
	if s.vcs == nil {
		return fmt.Errorf("fetch %s@%s: code reader is nil", repo, sha)
	}
	rc, err := s.vcs.Tarball(ctx, repo, sha)
	if err != nil {
		return fmt.Errorf("fetch %s@%s: %w", repo, sha, err)
	}
	if rc == nil {
		return fmt.Errorf("fetch %s@%s: tarball reader is nil", repo, sha)
	}
	if err := rc.Close(); err != nil {
		return fmt.Errorf("fetch %s@%s: close tarball: %w", repo, sha, err)
	}
	return nil
}

// localRunnerSandbox adapts *localrunner.Runner to ports.Sandbox and effects.OverlaySandbox.
type localRunnerSandbox struct {
	runner *localrunner.Runner
}

func (s *localRunnerSandbox) Run(ctx context.Context, j ports.SandboxJob) (ports.SandboxResult, error) {
	return s.runner.Run(ctx, j)
}

func (s *localRunnerSandbox) RunOverlay(ctx context.Context, j ports.SandboxJob, overlays []ports.File) (ports.SandboxResult, error) {
	return s.runner.RunJob(ctx, localrunner.Job{
		SandboxJob: j,
		Overlay:    overlays,
	})
}
