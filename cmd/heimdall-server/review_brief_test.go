package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/cardsfs"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/codegraph"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/knowledge"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/reviewbrief"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/reviewlearn"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/rulecard"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
)

const proposedCard = "---\ncheck: \"Is the cap summed?\"\nkind: judgement\nstatus: proposed\n---\nA body.\n"

type policyCode struct {
	reads int
	err   error
}

func (p *policyCode) ReadFile(context.Context, change.Repo, id.SHA, string) ([]byte, error) {
	p.reads++
	return []byte("policy"), p.err
}

func proposerRig(t *testing.T, proposals []reviewlearn.Proposal) (*cardProposer, *policyCode, *cardsfs.Store, *bytes.Buffer) {
	t.Helper()
	store, err := cardsfs.Open(t.TempDir())
	require.NoError(t, err)
	code := &policyCode{}
	logs := &bytes.Buffer{}
	return &cardProposer{
		proposals: func(context.Context) ([]reviewlearn.Proposal, error) { return proposals, nil },
		scopes: knowledge.PolicyScopes{Code: code, Decode: func([]byte) (policy.Policy, error) {
			return policy.Policy{People: map[string]policy.Person{"ana": {GitHub: "Ana-Dev"}, "bot": {Agent: "bot-reviewer"}}}, nil
		}},
		store: store, logger: slog.New(slog.NewTextHandler(logs, nil)),
	}, code, store, logs
}

func proposal(login, name, text string) reviewlearn.Proposal {
	return reviewlearn.Proposal{Repo: "acme/pay", BaseSHA: "aaaaaaa", Login: login, Name: name, Text: text}
}

func TestProposerWritesACardForTheMappedPersonOnce(t *testing.T) {
	p, code, store, _ := proposerRig(t, []reviewlearn.Proposal{proposal("ana-dev", "learned-1.md", proposedCard), proposal("ANA-DEV", "learned-2.md", proposedCard)})
	wrote, err := p.RunOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, 2, wrote, "the login match ignores case")
	require.Equal(t, 1, code.reads, "one policy is read once in a pass")
	text, err := store.Read("ana", "learned-1.md")
	require.NoError(t, err)
	require.Equal(t, proposedCard, text)

	wrote, err = p.RunOnce(context.Background())
	require.NoError(t, err)
	require.Zero(t, wrote)
}

// Rejected cards stay on disk, so the same proposal never comes back and an owner's edit stays.
func TestProposerNeverOverwritesACard(t *testing.T) {
	p, _, store, _ := proposerRig(t, []reviewlearn.Proposal{proposal("ana-dev", "learned-1.md", proposedCard)})
	rejected := "---\nstatus: rejected\n---\nno\n"
	require.NoError(t, store.Write("ana", "learned-1.md", []byte(rejected)))
	wrote, err := p.RunOnce(context.Background())
	require.NoError(t, err)
	require.Zero(t, wrote)
	text, err := store.Read("ana", "learned-1.md")
	require.NoError(t, err)
	require.Equal(t, rejected, text)
}

func TestProposerSkipsWhatItCannotPlaceAndKeepsGoing(t *testing.T) {
	sensitive := "Authorization: " + "Bearer abcdefgh12345678"
	p, _, store, logs := proposerRig(t, []reviewlearn.Proposal{
		proposal("stranger", "a.md", proposedCard),
		proposal("ana-dev", "active.md", "---\nstatus: active\n---\nThis would act at once.\n"),
		proposal("ana-dev", "broken.md", "---\nstatus: proposed\nno end"),
		proposal("ana-dev", "sensitive.md", "---\nstatus: proposed\n---\n"+sensitive+"\n"),
		proposal("ana-dev", "good.md", proposedCard),
	})
	wrote, err := p.RunOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, wrote)
	names, err := store.List("ana")
	require.NoError(t, err)
	require.Equal(t, []string{"good.md"}, names)
	require.Contains(t, logs.String(), "active.md")
	require.NotContains(t, logs.String(), "abcdefgh12345678", "a log never carries the sensitive text")
}

func TestProposerSurvivesAnUnreadablePolicy(t *testing.T) {
	p, code, store, logs := proposerRig(t, []reviewlearn.Proposal{proposal("ana-dev", "a.md", proposedCard)})
	code.err = errors.New("github down")
	wrote, err := p.RunOnce(context.Background())
	require.NoError(t, err)
	require.Zero(t, wrote)
	require.Contains(t, logs.String(), "policy unavailable")
	names, err := store.List("ana")
	require.NoError(t, err)
	require.Empty(t, names)
}

type tarSource struct {
	trees map[id.SHA]map[string]string
}

func (s tarSource) Tarball(_ context.Context, _ change.Repo, sha id.SHA) (io.ReadCloser, error) {
	files, ok := s.trees[sha]
	if !ok {
		return nil, errors.New("no such commit")
	}
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	w := tar.NewWriter(gz)
	for name, text := range files {
		if err := w.WriteHeader(&tar.Header{Name: "repo-abc/" + name, Mode: 0o644, Size: int64(len(text)), Typeflag: tar.TypeReg}); err != nil {
			return nil, err
		}
		if _, err := w.Write([]byte(text)); err != nil {
			return nil, err
		}
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	return io.NopCloser(&buf), nil
}

const moatDiff = "--- a/pay/refund.go\n+++ b/pay/refund.go\n@@ -1,3 +1,3 @@\n package pay\n \n-func Old() {}\n+func Refund() {}\n"

// The real graph behind the compiler's view: a removed symbol shows only when the base is indexed, a missing base
// keeps the head's map and marks the brief for a new compile, and a missing head loses only the map.
func TestGraphViewUsesTheRealGraphAndIsolatesTheBase(t *testing.T) {
	head := map[string]string{"pay/refund.go": "package pay\n\nfunc Refund() {}\n", "pay/caller.go": "package pay\n\nfunc use() { Old(); Refund() }\n"}
	base := map[string]string{"pay/refund.go": "package pay\n\nfunc Old() {}\n", "pay/caller.go": "package pay\n\nfunc use() { Old() }\n"}
	logger := slog.New(slog.DiscardHandler)
	compile := func(t *testing.T, trees map[id.SHA]map[string]string) reviewbrief.Brief {
		t.Helper()
		svc, err := codegraph.NewService(tarSource{trees: trees}, 4)
		require.NoError(t, err)
		compiler, err := reviewbrief.New(reviewbrief.Config{Logger: logger, Graph: graphView{svc: svc}, Cards: staleCards{}})
		require.NoError(t, err)
		return compiler.Compile(context.Background(), reviewbrief.Request{
			Person: "ana", Agent: "ana-reviewer", Diff: moatDiff,
			Task: workflow.TaskEnvelope{Role: workflow.RoleReviewer,
				Change: workflow.ChangeRef{Repo: "acme/pay", HeadSHA: "bbbbbbb", BaseSHA: "aaaaaaa"},
				Inputs: workflow.Inputs{FilesChanged: []string{"pay/refund.go"}}},
		})
	}

	whole := compile(t, map[id.SHA]map[string]string{"bbbbbbb": head, "aaaaaaa": base})
	require.False(t, whole.Partial, whole.ChangeMap)
	require.Contains(t, whole.ChangeMap, "Old")
	require.Contains(t, whole.ChangeMap, "pay/caller.go")
	require.Contains(t, whole.Obligations, "POSSIBLY STALE", "the card's symbol is gone at head")
	require.Equal(t, []string{"stale.md"}, whole.ObligationCards, "the card still ranks, through its path")

	noBase := compile(t, map[id.SHA]map[string]string{"bbbbbbb": head})
	require.True(t, noBase.Partial)
	require.Contains(t, noBase.ChangeMap, "base commit is not indexed")
	require.Contains(t, noBase.ChangeMap, "Refund")

	noHead := compile(t, map[id.SHA]map[string]string{"aaaaaaa": base})
	require.True(t, noHead.Partial)
	require.Equal(t, "Code graph unavailable: use read_code.", noHead.ChangeMap)
	require.Contains(t, noHead.Obligations, "stale.md", "the cards still rank on files without the graph")
	require.NotContains(t, noHead.Obligations, "POSSIBLY STALE", "no graph, no claim")
}

type staleCards struct{}

func (staleCards) All(string) ([]rulecard.Card, error) {
	return []rulecard.Card{{Name: "stale.md", Body: "b", Status: rulecard.StatusActive, AppliesTo: []string{"pay/**"}, Symbols: []string{"Vanished"}}}, nil
}

func TestPriorFindingsKeepOnlyDecidedOnes(t *testing.T) {
	for outcome, want := range map[string]bool{reviewlearn.OutcomeUseful: true, reviewlearn.OutcomeNoise: true, reviewlearn.OutcomeIgnored: false, reviewlearn.OutcomeNeutral: false, reviewlearn.OutcomePending: false} {
		_, decided := decidedOutcome(outcome)
		require.Equal(t, want, decided, outcome)
	}
}
