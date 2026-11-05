package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/engine"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/gateuse"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/intake"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/prsync"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
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
