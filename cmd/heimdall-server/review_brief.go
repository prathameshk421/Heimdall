package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/cardsfs"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/policyyaml"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/codegraph"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/knowledge"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/reviewbrief"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/reviewlearn"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/rulecard"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/pack"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/transport/mcp"
)

const (
	// graphCapacity is how many commit indexes stay in memory: about one change's base and head, twice.
	graphCapacity = 4
	// learningTTL is how long one read of the review history stays current.
	learningTTL = 60 * time.Second
	// maxBriefDiffBytes caps the diff text that the graph and the card triggers read.
	maxBriefDiffBytes = ports.MaxUntrustedText
)

// reviewMoat holds what the reviewer brief adds to the server: the brief, the code tools and the proposal worker.
type reviewMoat struct {
	briefs   mcp.Briefs
	graph    mcp.CodeGraph
	proposer *cardProposer
}

// newReviewMoat builds the code graph, the review history and the brief compiler. Without a cards directory the brief
// has no obligations and no card proposals are written. Every other part still works.
func newReviewMoat(d deps, ad adapters, history reviewlearn.History, blobs ports.BlobStore, memory reviewbrief.Memory, clk ports.Clock) (reviewMoat, error) {
	graph, err := codegraph.NewService(ad.vcs, graphCapacity)
	if err != nil {
		return reviewMoat{}, fmt.Errorf("build code graph: %w", err)
	}
	learning, err := reviewlearn.NewService(history, clk, learningTTL)
	if err != nil {
		return reviewMoat{}, fmt.Errorf("build review history: %w", err)
	}
	scopes := knowledge.PolicyScopes{Code: ad.vcs, Decode: policyyaml.Decode}
	cfg := reviewbrief.Config{Logger: d.logger, Graph: graphView{svc: graph}, Learning: learningView{svc: learning}, Memory: memory}
	moat := reviewMoat{graph: graph}
	if d.configuration.CardsDir != "" {
		store, err := cardsfs.Open(d.configuration.CardsDir)
		if err != nil {
			return reviewMoat{}, fmt.Errorf("open personal cards for the brief: %w", err)
		}
		cfg.Cards = store
		moat.proposer = &cardProposer{proposals: learningProposals(learning), scopes: scopes, store: store, logger: d.logger}
	}
	compiler, err := reviewbrief.New(cfg)
	if err != nil {
		return reviewMoat{}, fmt.Errorf("build brief compiler: %w", err)
	}
	moat.briefs = briefSource{compiler: compiler, scopes: scopes, blobs: blobs, logger: d.logger}
	return moat, nil
}

// briefSource prepares one compile: it resolves the person and reads the diff. A failure in either costs that part of
// the brief and nothing else.
type briefSource struct {
	compiler *reviewbrief.Compiler
	scopes   knowledge.PolicyScopes
	blobs    ports.BlobStore
	logger   *slog.Logger
}

// Brief compiles the brief of one assignment. The person comes from the base-commit policy, as for read_card, so a
// reviewer sees only the cards of the person it speaks for.
func (s briefSource) Brief(ctx context.Context, p mcp.Principal, a mcp.Assignment) (reviewbrief.Brief, error) {
	req := reviewbrief.Request{Agent: p.Agent, Task: a.Task, Diff: s.diff(ctx, a)}
	if a.Task.Role == workflow.RoleReviewer {
		person, _, err := s.scopes.PersonForAgent(ctx, p.Agent, change.Repo(a.Task.Change.Repo), a.Task.Change.BaseSHA)
		if err != nil {
			s.logger.WarnContext(ctx, "reviewer person unavailable for brief", "agent", string(p.Agent), "error", err)
		}
		req.Person = person
	}
	return s.compiler.Compile(ctx, req), nil
}

func (s briefSource) diff(ctx context.Context, a mcp.Assignment) string {
	if a.Task.Inputs.Diff == "" {
		return ""
	}
	text, err := readCapped(ctx, s.blobs, a.Task.Inputs.Diff, maxBriefDiffBytes)
	if err != nil {
		s.logger.WarnContext(ctx, "diff unavailable for brief", "task", string(a.Task.TaskID), "error", err)
		return ""
	}
	return text
}

func readCapped(ctx context.Context, blobs ports.BlobStore, ref workflow.BlobRef, limit int64) (string, error) {
	rc, err := blobs.Get(ctx, ref)
	if err != nil {
		return "", fmt.Errorf("get blob: %w", err)
	}
	defer func() { _ = rc.Close() }() // a close after a read has nothing left to report
	content, err := io.ReadAll(io.LimitReader(rc, limit))
	if err != nil {
		return "", fmt.Errorf("read blob: %w", err)
	}
	return string(content), nil
}

// graphView adapts the code graph to the compiler. It reads both commits at the same time, so a slow base does not
// start late. A missing base only loses the removed symbols, and impactView says so.
type graphView struct {
	svc *codegraph.Service
}

// Impact implements reviewbrief.Graph.
func (g graphView) Impact(ctx context.Context, repo change.Repo, head, base id.SHA, diff string, files []string) (reviewbrief.Impact, error) {
	type result struct {
		idx *codegraph.Index
		err error
	}
	baseDone := make(chan result, 1)
	go func() {
		idx, err := g.svc.Index(ctx, repo, base)
		baseDone <- result{idx, err}
	}()
	headIdx, err := g.svc.Index(ctx, repo, head)
	baseRes := <-baseDone
	if err != nil {
		return nil, fmt.Errorf("index the head commit: %w", err)
	}
	return impactView{ImpactMap: codegraph.Impact(headIdx, baseRes.idx, diff, files), head: headIdx, noBase: baseRes.err != nil}, nil
}

// impactView adds what the compiler needs from the head index to the impact map.
type impactView struct {
	codegraph.ImpactMap
	head   *codegraph.Index
	noBase bool
}

// Defined implements reviewbrief.Impact.
func (v impactView) Defined(symbol string) bool { return len(v.head.Definitions(symbol)) > 0 }

// Partial implements reviewbrief.Impact.
func (v impactView) Partial() bool { return v.head.Truncated() }

// BaseMissing implements reviewbrief.Impact.
func (v impactView) BaseMissing() bool { return v.noBase }

// Render implements reviewbrief.Impact and says when the base was not ready.
func (v impactView) Render(maxBytes int) string {
	if !v.noBase {
		return v.ImpactMap.Render(maxBytes)
	}
	const note = "The base commit is not indexed yet, so removed symbols are not listed. Call get_task again later to see them.\n"
	return note + v.ImpactMap.Render(maxBytes-len(note))
}

// learningView adapts the review history to the compiler.
type learningView struct {
	svc *reviewlearn.Service
}

// Snapshot implements reviewbrief.Learning.
func (l learningView) Snapshot(ctx context.Context) (reviewbrief.History, error) {
	snap, err := l.svc.Snapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("read review history: %w", err)
	}
	return learnedHistory{snap: snap}, nil
}

// learnedHistory adapts one snapshot to the compiler.
type learnedHistory struct {
	snap *reviewlearn.Snapshot
}

// CardRecords implements reviewbrief.History.
func (h learnedHistory) CardRecords(agent id.AgentName) map[string]rulecard.Record {
	return h.snap.CardRecords(agent)
}

// Calibration implements reviewbrief.History.
func (h learnedHistory) Calibration(agent id.AgentName) reviewbrief.Calibration {
	c := h.snap.Calibration(agent)
	return reviewbrief.Calibration{Useful: c.Useful, Noise: c.Noise, Ignored: c.Ignored, NoisyCategories: c.NoisyCategories, ResolutionRate: c.ResolutionRate}
}

// PriorFindings implements reviewbrief.History. The snapshot prefers matching files but also returns others, so this
// keeps only decided findings on the files of this change.
func (h learnedHistory) PriorFindings(repo change.Repo, files []string, exclude id.ChangeID, limit int) []reviewbrief.PriorFinding {
	var out []reviewbrief.PriorFinding
	for _, f := range h.snap.PriorFindings(repo, files, exclude, -1) {
		outcome, decided := decidedOutcome(f.Outcome)
		if !decided || !slices.Contains(files, f.File) {
			continue
		}
		if len(out) == limit {
			break
		}
		out = append(out, reviewbrief.PriorFinding{Change: f.Change, Ref: f.Ref, File: f.File, Claim: f.Claim, Category: f.Category, Outcome: outcome, Reason: f.Reason})
	}
	return out
}

// decidedOutcome returns the words a reviewer reads for an outcome that someone decided. An ignored finding had no answer.
func decidedOutcome(outcome string) (string, bool) {
	switch outcome {
	case reviewlearn.OutcomeUseful:
		return "upheld or fixed", true
	case reviewlearn.OutcomeNoise:
		return "dismissed or withdrawn", true
	}
	return "", false
}

func learningProposals(svc *reviewlearn.Service) func(context.Context) ([]reviewlearn.Proposal, error) {
	return func(ctx context.Context) ([]reviewlearn.Proposal, error) {
		snap, err := svc.Snapshot(ctx)
		if err != nil {
			return nil, fmt.Errorf("read review history: %w", err)
		}
		return snap.Proposals(), nil
	}
}

// cardProposer writes the cards that human decisions suggest into the folder of the person who decided. A card starts
// as proposed, so it takes no part in reviews until its owner accepts it. A card that already has that name is left
// alone, so a rejected card stays rejected and an edited card keeps its edits.
type cardProposer struct {
	proposals func(context.Context) ([]reviewlearn.Proposal, error)
	scopes    knowledge.PolicyScopes
	store     *cardsfs.Store
	logger    *slog.Logger
}

type policySource struct {
	repo change.Repo
	sha  id.SHA
}

// RunOnce writes every proposal that has an owner and no card yet, and returns how many it wrote. One bad proposal is
// logged and the others go on.
func (c *cardProposer) RunOnce(ctx context.Context) (int, error) {
	proposals, err := c.proposals(ctx)
	if err != nil {
		return 0, err
	}
	people := map[policySource]map[string]string{}
	written := 0
	for _, proposal := range proposals {
		person, ok := c.owner(ctx, proposal, people)
		if !ok {
			continue
		}
		wrote, err := c.write(person, proposal)
		if err != nil {
			c.logger.WarnContext(ctx, "card proposal skipped", "card", proposal.Name, "error", err)
			continue
		}
		if wrote {
			written++
		}
	}
	return written, nil
}

// owner maps the proposal's login to a person through the policy at its base commit. Each policy is read once in a pass.
func (c *cardProposer) owner(ctx context.Context, p reviewlearn.Proposal, people map[policySource]map[string]string) (string, bool) {
	key := policySource{p.Repo, p.BaseSHA}
	byLogin, read := people[key]
	if !read {
		var err error
		byLogin, err = c.peopleAt(ctx, key)
		if err != nil {
			c.logger.WarnContext(ctx, "policy unavailable for card proposals", "repo", string(p.Repo), "error", err)
		}
		people[key] = byLogin
	}
	person, ok := byLogin[strings.ToLower(p.Login)]
	return person, ok
}

func (c *cardProposer) peopleAt(ctx context.Context, key policySource) (map[string]string, error) {
	raw, err := c.scopes.Code.ReadFile(ctx, key.repo, key.sha, "heimdall.yaml")
	if err != nil {
		return nil, fmt.Errorf("read policy: %w", err)
	}
	p, err := c.scopes.Decode(raw)
	if err != nil {
		return nil, fmt.Errorf("decode policy: %w", err)
	}
	byLogin := make(map[string]string, len(p.People))
	for name, person := range p.People {
		if person.GitHub != "" {
			byLogin[strings.ToLower(person.GitHub)] = name
		}
	}
	return byLogin, nil
}

// write stores one proposal under the same checks as a card a person uploads. It refuses text that would act at once,
// so this path can never add an active card.
func (c *cardProposer) write(person string, p reviewlearn.Proposal) (bool, error) {
	_, err := c.store.Read(person, p.Name)
	if err == nil {
		return false, nil
	}
	if !errors.Is(err, cardsfs.ErrNotFound) {
		return false, fmt.Errorf("check for card: %w", err)
	}
	card, err := cardsfs.Parse(p.Name, p.Text)
	if err != nil {
		return false, fmt.Errorf("parse proposal: %w", err)
	}
	if card.Status != rulecard.StatusProposed {
		return false, fmt.Errorf("proposal has status %q, want %q", card.Status, rulecard.StatusProposed)
	}
	if _, found := pack.RedactReport(p.Text); len(found) > 0 {
		return false, fmt.Errorf("proposal looks like it holds sensitive text (%d matches)", len(found))
	}
	if err := c.store.Write(person, p.Name, []byte(p.Text)); err != nil {
		return false, fmt.Errorf("store card: %w", err)
	}
	return true, nil
}
