package reviewbrief_test

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/reviewbrief"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/rulecard"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

var errDown = errors.New("source is down")

type fakeCards struct {
	cards []rulecard.Card
	err   error
}

func (f fakeCards) All(string) ([]rulecard.Card, error) { return f.cards, f.err }

type fakeImpact struct {
	symbols, dependents []string
	defined             map[string]bool
	partial, noBase     bool
}

func (f fakeImpact) Render(maxBytes int) string { return strings.Repeat("m", min(maxBytes, 9000)) }
func (f fakeImpact) Symbols() []string          { return f.symbols }
func (f fakeImpact) DependentFiles() []string   { return f.dependents }
func (f fakeImpact) Defined(name string) bool   { return f.defined[name] }
func (f fakeImpact) Partial() bool              { return f.partial }
func (f fakeImpact) BaseMissing() bool          { return f.noBase }

type fakeGraph struct {
	impact reviewbrief.Impact
	err    error
	block  bool
}

func (f fakeGraph) Impact(ctx context.Context, _ change.Repo, _, _ id.SHA, _ string, _ []string) (reviewbrief.Impact, error) {
	if f.block {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return f.impact, f.err
}

type fakeHistory struct {
	records map[string]rulecard.Record
	cal     reviewbrief.Calibration
	prior   []reviewbrief.PriorFinding
}

func (f fakeHistory) CardRecords(id.AgentName) map[string]rulecard.Record { return f.records }
func (f fakeHistory) Calibration(id.AgentName) reviewbrief.Calibration    { return f.cal }
func (f fakeHistory) PriorFindings(_ change.Repo, _ []string, _ id.ChangeID, limit int) []reviewbrief.PriorFinding {
	return f.prior[:min(limit, len(f.prior))]
}

type fakeLearning struct {
	history reviewbrief.History
	err     error
}

func (f fakeLearning) Snapshot(context.Context) (reviewbrief.History, error) { return f.history, f.err }

type fakeMemory struct {
	items []ports.MemoryItem
	err   error
	block bool
	got   *ports.MemoryQuery
}

func (f fakeMemory) Ask(ctx context.Context, _ id.AgentName, _ id.TaskID, _ change.Repo, _ id.SHA, q ports.MemoryQuery) ([]ports.MemoryItem, error) {
	if f.got != nil {
		*f.got = q
	}
	if f.block {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return f.items, f.err
}

func reviewerRequest() reviewbrief.Request {
	return reviewbrief.Request{
		Person: "ana", Agent: "ana-reviewer",
		Task: workflow.TaskEnvelope{
			TaskID: "tsk_1", Role: workflow.RoleReviewer,
			Change: workflow.ChangeRef{ID: "PAY-1", Repo: "acme/pay", BaseSHA: "aaaaaaa", HeadSHA: "bbbbbbb"},
			Inputs: workflow.Inputs{FilesChanged: []string{"internal/refunds/service.go"}},
		},
		Diff: "+++ b/internal/refunds/service.go\n+func refund(amount int) {}\n-old line\n",
	}
}

func compiler(t *testing.T, cfg reviewbrief.Config) *reviewbrief.Compiler {
	t.Helper()
	cfg.Logger = slog.New(slog.DiscardHandler)
	c, err := reviewbrief.New(cfg)
	require.NoError(t, err)
	return c
}

func activeCard(name string) rulecard.Card {
	return rulecard.Card{Name: name, Body: "body of " + name, Status: rulecard.StatusActive, AppliesTo: []string{"internal/refunds/**"}}
}

func TestEachSectionFailsAlone(t *testing.T) {
	cases := []struct {
		name string
		cfg  reviewbrief.Config
		want func(t *testing.T, b reviewbrief.Brief)
	}{
		{"graph", reviewbrief.Config{Graph: fakeGraph{err: errDown}}, func(t *testing.T, b reviewbrief.Brief) {
			require.Equal(t, "Code graph unavailable: use read_code.", b.ChangeMap)
			require.Contains(t, b.Obligations, "refund.md", "the cards still rank on files and added text")
			require.Contains(t, b.Memory, "m_1")
			require.Contains(t, b.PriorFindings, "F3")
		}},
		{"cards", reviewbrief.Config{Cards: fakeCards{err: errDown}}, func(t *testing.T, b reviewbrief.Brief) {
			require.Equal(t, "Personal cards unavailable: use read_card.", b.Obligations)
			require.Empty(t, b.ObligationCards)
			require.NotEmpty(t, b.ChangeMap)
			require.Contains(t, b.Record, "Your record")
		}},
		{"learning", reviewbrief.Config{Learning: fakeLearning{err: errDown}}, func(t *testing.T, b reviewbrief.Brief) {
			require.Equal(t, "Prior findings unavailable.", b.PriorFindings)
			require.Empty(t, b.Record)
			require.Contains(t, b.Obligations, "refund.md", "the cards still rank without records")
		}},
		{"memory", reviewbrief.Config{Memory: fakeMemory{err: errDown}}, func(t *testing.T, b reviewbrief.Brief) {
			require.Equal(t, "Team memory unavailable: carry on without it.", b.Memory)
			require.Contains(t, b.Obligations, "refund.md")
			require.NotEmpty(t, b.ChangeMap)
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := reviewbrief.Config{
				Cards: fakeCards{cards: []rulecard.Card{activeCard("refund.md")}},
				Graph: fakeGraph{impact: fakeImpact{symbols: []string{"Refund"}}},
				Learning: fakeLearning{history: fakeHistory{
					cal:   reviewbrief.Calibration{Useful: 1},
					prior: []reviewbrief.PriorFinding{{Change: "PAY-0", Ref: "F3", Outcome: "dismissed"}},
				}},
				Memory: fakeMemory{items: []ports.MemoryItem{{Ref: "m_1", Label: ports.LabelConfirmed, Text: "fact"}}},
			}
			switch c.name {
			case "graph":
				cfg.Graph = c.cfg.Graph
			case "cards":
				cfg.Cards = c.cfg.Cards
			case "learning":
				cfg.Learning = c.cfg.Learning
			case "memory":
				cfg.Memory = c.cfg.Memory
			}
			b := compiler(t, cfg).Compile(context.Background(), reviewerRequest())
			require.True(t, b.Partial, "a failed section marks the brief so it is not reused")
			c.want(t, b)
		})
	}
}

// A source that hangs costs its own deadline, not the task.
func TestAHungSourceHitsItsDeadline(t *testing.T) {
	b := compiler(t, reviewbrief.Config{
		Graph:     fakeGraph{block: true},
		Memory:    fakeMemory{block: true},
		GraphWait: 10 * time.Millisecond, MemoryWait: 10 * time.Millisecond,
	}).Compile(context.Background(), reviewerRequest())
	require.Equal(t, "Code graph unavailable: use read_code.", b.ChangeMap)
	require.Equal(t, "Team memory unavailable: carry on without it.", b.Memory)
	require.True(t, b.Partial)
}

func TestAnUnresolvedPersonLosesOnlyTheCards(t *testing.T) {
	req := reviewerRequest()
	req.Person = ""
	b := compiler(t, reviewbrief.Config{
		Cards: fakeCards{cards: []rulecard.Card{activeCard("refund.md")}},
		Graph: fakeGraph{impact: fakeImpact{}},
	}).Compile(context.Background(), req)
	require.Equal(t, "Personal cards unavailable: use read_card.", b.Obligations)
	require.NotEmpty(t, b.ChangeMap)
}

func TestSourcesThatAreOffGiveEmptySectionsAndAWholeBrief(t *testing.T) {
	b := compiler(t, reviewbrief.Config{}).Compile(context.Background(), reviewerRequest())
	require.Equal(t, reviewbrief.Brief{}, b)
}

func TestAResolverGetsOnlyTheChangeMapAndPriorFindings(t *testing.T) {
	req := reviewerRequest()
	req.Task.Role = workflow.RoleResolver
	req.Person = ""
	b := compiler(t, reviewbrief.Config{
		Cards:    fakeCards{err: errDown},
		Graph:    fakeGraph{impact: fakeImpact{}},
		Learning: fakeLearning{history: fakeHistory{cal: reviewbrief.Calibration{Useful: 1}, prior: []reviewbrief.PriorFinding{{Ref: "F3"}}}},
		Memory:   fakeMemory{err: errDown},
	}).Compile(context.Background(), req)
	require.NotEmpty(t, b.ChangeMap)
	require.Contains(t, b.PriorFindings, "F3")
	require.Empty(t, b.Obligations)
	require.Empty(t, b.Memory)
	require.Empty(t, b.Record)
	require.False(t, b.Partial)
}

func TestStaleFlag(t *testing.T) {
	card := rulecard.Card{Name: "cap.md", Body: "b", Status: rulecard.StatusActive, Symbols: []string{"RefundCap", "Gone"}, AppliesTo: []string{"internal/**"}}
	general := activeCard("plain.md")
	cases := []struct {
		name   string
		impact fakeImpact
		stale  bool
	}{
		{"no symbol defined", fakeImpact{}, true},
		{"one symbol defined", fakeImpact{defined: map[string]bool{"RefundCap": true}}, false},
		{"a truncated index proves nothing", fakeImpact{partial: true}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := compiler(t, reviewbrief.Config{
				Cards: fakeCards{cards: []rulecard.Card{card, general}},
				Graph: fakeGraph{impact: c.impact},
			}).Compile(context.Background(), reviewerRequest())
			require.Equal(t, c.stale, strings.Contains(b.Obligations, "POSSIBLY STALE"), b.Obligations)
			require.Equal(t, 1, strings.Count(b.Obligations, "## plain.md"))
		})
	}
	t.Run("a card with no symbols is never stale", func(t *testing.T) {
		b := compiler(t, reviewbrief.Config{Cards: fakeCards{cards: []rulecard.Card{general}}, Graph: fakeGraph{impact: fakeImpact{}}}).
			Compile(context.Background(), reviewerRequest())
		require.NotContains(t, b.Obligations, "POSSIBLY STALE")
	})
	t.Run("no graph gives no flag", func(t *testing.T) {
		b := compiler(t, reviewbrief.Config{Cards: fakeCards{cards: []rulecard.Card{card}}}).Compile(context.Background(), reviewerRequest())
		require.Contains(t, b.Obligations, "cap.md")
		require.NotContains(t, b.Obligations, "POSSIBLY STALE")
	})
}

// The rank decides the order, the graph and the diff feed the rank, and the records move a noisy card down.
func TestObligationsFollowTheRankAndUseEveryFact(t *testing.T) {
	bySymbol := rulecard.Card{Name: "by-symbol.md", Body: "b", Status: rulecard.StatusActive, Symbols: []string{"Create"}}
	byTrigger := rulecard.Card{Name: "by-trigger.md", Body: "b", Status: rulecard.StatusActive, Triggers: []string{"refund("}}
	byDependent := rulecard.Card{Name: "by-dependent.md", Body: "b", Status: rulecard.StatusActive, AppliesTo: []string{"api/**"}}
	unrelated := rulecard.Card{Name: "unrelated.md", Body: "b", Status: rulecard.StatusActive, AppliesTo: []string{"docs/**"}}
	proposed := rulecard.Card{Name: "proposed.md", Body: "b", Status: rulecard.StatusProposed}
	b := compiler(t, reviewbrief.Config{
		Cards: fakeCards{cards: []rulecard.Card{unrelated, proposed, byDependent, byTrigger, bySymbol}},
		Graph: fakeGraph{impact: fakeImpact{symbols: []string{"Create"}, dependents: []string{"api/routes.go"}, defined: map[string]bool{"Create": true}}},
	}).Compile(context.Background(), reviewerRequest())
	require.Equal(t, []string{"by-symbol.md", "by-trigger.md", "by-dependent.md"}, b.ObligationCards)
	require.Less(t, strings.Index(b.Obligations, "## by-symbol.md"), strings.Index(b.Obligations, "## by-trigger.md"))
	require.Less(t, strings.Index(b.Obligations, "## by-trigger.md"), strings.Index(b.Obligations, "## by-dependent.md"))
	require.True(t, strings.HasSuffix(b.Obligations, "It is not evidence."), "the section ends with the instruction")

	noisy := fakeHistory{records: map[string]rulecard.Record{"by-symbol.md": {Noise: 2}}}
	b = compiler(t, reviewbrief.Config{
		Cards:    fakeCards{cards: []rulecard.Card{byTrigger, bySymbol}},
		Graph:    fakeGraph{impact: fakeImpact{symbols: []string{"Create"}}},
		Learning: fakeLearning{history: noisy},
	}).Compile(context.Background(), reviewerRequest())
	require.Equal(t, []string{"by-trigger.md", "by-symbol.md"}, b.ObligationCards, "a dismissed card ranks lower")
}

func TestCardLinesRenderWithDefaults(t *testing.T) {
	card := activeCard("cap.md")
	card.Kind, card.Source = "invariant", "session 2026-10-30"
	b := compiler(t, reviewbrief.Config{Cards: fakeCards{cards: []rulecard.Card{card}}}).Compile(context.Background(), reviewerRequest())
	require.Contains(t, b.Obligations, "## cap.md (invariant) [from: session 2026-10-30]\n")
	require.Contains(t, b.Obligations, "WHY: the change touches internal/refunds/service.go\n")
	require.Contains(t, b.Obligations, "CHECK: state what would violate this\n")
	require.NotContains(t, b.Obligations, "PROOF: ", "a card with no proof hint has no proof line")

	card.Check, card.Proof = "Does the cap count refunds?", "Test two refunds."
	b = compiler(t, reviewbrief.Config{Cards: fakeCards{cards: []rulecard.Card{card}}}).Compile(context.Background(), reviewerRequest())
	require.Contains(t, b.Obligations, "CHECK: Does the cap count refunds?\n")
	require.Contains(t, b.Obligations, "PROOF: Test two refunds.\n")
}

// Twelve cards of 1 KiB fit the rank's body budget, but their extra lines do not fit the section cap. The names must
// match the text exactly, because submit_review checks rule_checks against them.
func TestObligationsStayInsideTheCapAndNameOnlyWhatTheyShow(t *testing.T) {
	var cards []rulecard.Card
	for _, name := range []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l"} {
		card := activeCard(name + ".md")
		card.Body = strings.Repeat("x", 1000)
		card.Check = strings.Repeat("q", 250)
		cards = append(cards, card)
	}
	b := compiler(t, reviewbrief.Config{Cards: fakeCards{cards: cards}}).Compile(context.Background(), reviewerRequest())
	require.LessOrEqual(t, len(b.Obligations), 12<<10)
	require.Less(t, len(b.ObligationCards), len(cards))
	require.NotEmpty(t, b.ObligationCards)
	for _, card := range cards {
		shown := strings.Contains(b.Obligations, "## "+card.Name)
		require.Equal(t, shown, contains(b.ObligationCards, card.Name), card.Name)
	}
	require.Contains(t, b.Obligations, "more cards did not fit")
	require.True(t, strings.HasSuffix(b.Obligations, "It is not evidence."))
}

func contains(names []string, name string) bool {
	for _, n := range names {
		if n == name {
			return true
		}
	}
	return false
}

// A map without the base leaves out removed symbols, so the brief is compiled again next time.
func TestAMissingBaseMarksTheBriefPartial(t *testing.T) {
	b := compiler(t, reviewbrief.Config{Graph: fakeGraph{impact: fakeImpact{noBase: true}}}).Compile(context.Background(), reviewerRequest())
	require.True(t, b.Partial)
	require.NotEmpty(t, b.ChangeMap)
}

func TestChangeMapIsCapped(t *testing.T) {
	b := compiler(t, reviewbrief.Config{Graph: fakeGraph{impact: fakeImpact{}}}).Compile(context.Background(), reviewerRequest())
	require.Len(t, b.ChangeMap, 6<<10)
}

func TestPriorFindingsAreCappedAndCarryTheInstruction(t *testing.T) {
	var prior []reviewbrief.PriorFinding
	for range 9 {
		prior = append(prior, reviewbrief.PriorFinding{Change: "PAY-0", Ref: "F1", File: "a.go", Category: "correctness", Outcome: "dismissed",
			Claim: strings.Repeat("c", 900), Reason: "not\nreal " + strings.Repeat("r", 900)})
	}
	b := compiler(t, reviewbrief.Config{Learning: fakeLearning{history: fakeHistory{prior: prior}}}).Compile(context.Background(), reviewerRequest())
	require.Equal(t, 6, strings.Count(b.PriorFindings, "- PAY-0"))
	require.Contains(t, b.PriorFindings, "dismissed one again without new evidence")
	require.Contains(t, b.PriorFindings, "upheld one for a regression")
	require.Contains(t, b.PriorFindings, "not real ", "a reason stays on one line")
	require.Less(t, len(b.PriorFindings), 6*800)
}

func TestMemoryQueryStaysInsideTheToolLimits(t *testing.T) {
	var symbols []string
	for i := range 40 {
		symbols = append(symbols, strings.Repeat("s", 60)+string(rune('a'+i%26))+string(rune('a'+i/26)))
	}
	symbols = append([]string{strings.Repeat("x", 129), " "}, symbols...)
	var got ports.MemoryQuery
	req := reviewerRequest()
	compiler(t, reviewbrief.Config{
		Graph:  fakeGraph{impact: fakeImpact{symbols: symbols}},
		Memory: fakeMemory{got: &got},
	}).Compile(context.Background(), req)
	require.LessOrEqual(t, len(got.Question), 1024)
	require.NotEmpty(t, got.Question)
	require.LessOrEqual(t, len(got.Entities), 20)
	require.Contains(t, got.Entities, req.Task.Inputs.FilesChanged[0], "a file is asked about even with many symbols")
	for _, entity := range got.Entities {
		require.NotEmpty(t, strings.TrimSpace(entity))
		require.LessOrEqual(t, len(entity), 128)
	}
	require.Equal(t, []string{}, got.Groups, "the knowledge service sets the groups, never the caller")
	require.Positive(t, got.Limit)
	require.LessOrEqual(t, got.Limit, 20)
}

func TestMemoryRendersLabelsAndCaps(t *testing.T) {
	var items []ports.MemoryItem
	for range 20 {
		items = append(items, ports.MemoryItem{Ref: "m_1", Label: ports.LabelObserved, Text: strings.Repeat("t", 2000)})
	}
	b := compiler(t, reviewbrief.Config{Memory: fakeMemory{items: items}}).Compile(context.Background(), reviewerRequest())
	require.Contains(t, b.Memory, "m_1 (observed)")
	require.Contains(t, b.Memory, "Only a ref labelled confirmed")
	require.LessOrEqual(t, len(b.Memory), 4<<10)
	require.False(t, b.Partial)

	empty := compiler(t, reviewbrief.Config{Memory: fakeMemory{}}).Compile(context.Background(), reviewerRequest())
	require.Contains(t, empty.Memory, "Silence is not a pass")
}

func TestRecordNamesNoisyCategories(t *testing.T) {
	render := func(cal reviewbrief.Calibration) string {
		return compiler(t, reviewbrief.Config{Learning: fakeLearning{history: fakeHistory{cal: cal}}}).
			Compile(context.Background(), reviewerRequest()).Record
	}
	require.Empty(t, render(reviewbrief.Calibration{}), "no history means no record")
	text := render(reviewbrief.Calibration{Useful: 4, Noise: 2, Ignored: 1, ResolutionRate: 0.5, NoisyCategories: []string{"style", "ops"}})
	require.Len(t, strings.Split(text, "\n"), 2)
	require.Contains(t, text, "4 useful, 2 dismissed, 1 ignored, 50% resolved")
	require.Contains(t, text, "style, ops. Be stricter before filing these.")
	require.Len(t, strings.Split(render(reviewbrief.Calibration{Useful: 1}), "\n"), 2)
}
