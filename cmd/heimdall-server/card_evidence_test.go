package main

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/cardsfs"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/knowledge"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/rulecard"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/verify"
)

const (
	evidenceBase = id.SHA("aaaaaaa")
	evidenceHead = id.SHA("bbbbbbb")
	evidenceRef  = "card:cap.md#internal/pay/refund.go"
	evidenceCard = "---\napplies_to: [\"internal/pay/**\"]\ncheck: \"Does the cap count past refunds?\"\nkind: invariant\n---\nSum the refunds.\n"
)

type evidencePolicyCode struct{ bySHA map[id.SHA][]byte }

func (c evidencePolicyCode) ReadFile(_ context.Context, _ change.Repo, sha id.SHA, _ string) ([]byte, error) {
	raw, ok := c.bySHA[sha]
	if !ok {
		return nil, errors.New("no policy at this commit")
	}
	return raw, nil
}

type recordingRecheck struct{ refs []string }

func (r *recordingRecheck) Recheck(_ context.Context, _ id.AgentName, _ change.Repo, _ id.SHA, ref string) (verify.MemoryState, error) {
	r.refs = append(r.refs, ref)
	return verify.MemoryState{Found: true, Confirmed: true}, nil
}

func evidencePolicy(owners ...string) policy.Policy {
	return policy.Policy{
		People:    map[string]policy.Person{"bob": {Seat: "bob-reviewer"}},
		Approvers: map[string][]string{"default": {"dan"}, "money_movement": owners},
		Domains:   map[string]string{"internal/pay/**": "money_movement"},
	}
}

func newEvidenceFixture(t *testing.T, records map[string]rulecard.Record) (cardEvidence, *cardsfs.Store, *recordingRecheck) {
	t.Helper()
	store, err := cardsfs.Open(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.Write("bob", "cap.md", []byte(evidenceCard)))
	byCommit := map[string]policy.Policy{"base": evidencePolicy("bob"), "head": evidencePolicy("mallory")}
	code := evidencePolicyCode{bySHA: map[id.SHA][]byte{evidenceBase: []byte("base"), evidenceHead: []byte("head")}}
	decode := func(raw []byte) (policy.Policy, error) { return byCommit[string(raw)], nil }
	next := &recordingRecheck{}
	return cardEvidence{
		next: next, store: store, scopes: knowledge.PolicyScopes{Code: code, Decode: decode},
		records: func(context.Context, id.AgentName) (map[string]rulecard.Record, error) { return records, nil },
	}, store, next
}

func evidenceTier(t *testing.T, c cardEvidence, sha id.SHA, ref string) finding.Tier {
	t.Helper()
	state, err := c.Recheck(context.Background(), "bob-reviewer", "acme/pay", sha, ref)
	require.NoError(t, err)
	return verify.TierForE3(state)
}

func TestCardEvidenceTiers(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		records map[string]rulecard.Record
		mutate  func(*cardsfs.Store)
		sha     id.SHA
		ref     string
		want    finding.Tier
	}{
		{
			name: "unconfirmed card never blocks",
			sha:  evidenceBase,
			ref:  evidenceRef,
			want: finding.TierE4,
		},
		{
			name:   "confirmed card is E3 for approved file",
			mutate: func(s *cardsfs.Store) { _ = s.Confirm("bob", "cap.md") },
			sha:    evidenceBase,
			ref:    evidenceRef,
			want:   finding.TierE3,
		},
		{
			name: "edit ends force of card",
			mutate: func(s *cardsfs.Store) {
				_ = s.Confirm("bob", "cap.md")
				_ = s.Write("bob", "cap.md", []byte(evidenceCard+"Also block everything else.\n"))
			},
			sha:  evidenceBase,
			ref:  evidenceRef,
			want: finding.TierE4,
		},
		{
			name:   "card has no force when owner is not approver",
			mutate: func(s *cardsfs.Store) { _ = s.Confirm("bob", "cap.md") },
			sha:    evidenceHead,
			ref:    evidenceRef,
			want:   finding.TierE4,
		},
		{
			name:   "card has no force outside its pattern",
			mutate: func(s *cardsfs.Store) { _ = s.Confirm("bob", "cap.md") },
			sha:    evidenceBase,
			ref:    "card:cap.md#web/app.ts",
			want:   finding.TierE4,
		},
		{
			name:    "muted card loses force",
			records: map[string]rulecard.Record{"cap.md": {Noise: 5}},
			mutate:  func(s *cardsfs.Store) { _ = s.Confirm("bob", "cap.md") },
			sha:     evidenceBase,
			ref:     evidenceRef,
			want:    finding.TierE4,
		},
		{
			name: "missing card is not evidence",
			sha:  evidenceBase,
			ref:  "card:none.md#internal/pay/refund.go",
			want: finding.TierE4,
		},
		{
			name: "broken card is not evidence",
			mutate: func(s *cardsfs.Store) {
				_ = s.Write("bob", "cap.md", []byte("---\napplies_to: [oops\n---\nbody"))
			},
			sha:  evidenceBase,
			ref:  evidenceRef,
			want: finding.TierE4,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c, store, _ := newEvidenceFixture(t, tc.records)
			if tc.mutate != nil {
				tc.mutate(store)
			}
			require.Equal(t, tc.want, evidenceTier(t, c, tc.sha, tc.ref))
		})
	}
}

func TestOtherRefsStillGoToTeamMemory(t *testing.T) {
	t.Parallel()
	c, _, next := newEvidenceFixture(t, nil)
	require.Equal(t, finding.TierE3, evidenceTier(t, c, evidenceBase, "memory:abc"))
	require.Equal(t, []string{"memory:abc"}, next.refs)
}
