package taskinput

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
)

type blobs map[workflow.BlobRef]string

func (b blobs) Get(_ context.Context, ref workflow.BlobRef) (io.ReadCloser, error) {
	text, ok := b[ref]
	if !ok {
		return nil, errors.New("missing")
	}
	return io.NopCloser(strings.NewReader(text)), nil
}

func wrapFake(label string, text []byte) (string, error) {
	return "[" + label + "]" + string(text), nil
}

func deps(b blobs) Deps { return Deps{Blobs: b, Wrap: wrapFake} }

const ref = workflow.BlobRef("blob:sha256:0000000000000000000000000000000000000000000000000000000000000001")

func TestEveryInputOfEveryKindHasARenderer(t *testing.T) {
	names := Names()
	for _, k := range workflow.Kinds() {
		for _, in := range k.Inputs {
			require.Contains(t, names, in, "kind %s", k.Name)
		}
	}
}

func TestRenderKeepsTheOrderOfTheKindAndTurnsAFailureIntoANote(t *testing.T) {
	kind := workflow.Kind{Inputs: []string{"ticket", "diff", "rejection", "nonsense"}}
	s := Source{Rejection: []string{"too weak"}, Attempt: 2}
	s.Task.Inputs.Diff = ref // not in the blob store: this section fails
	got := Render(context.Background(), kind, s, deps(blobs{}))
	require.Len(t, got, 4)
	require.Equal(t, []string{"ticket", "diff", "rejection", "nonsense"}, []string{got[0].Name, got[1].Name, got[2].Name, got[3].Name})
	require.Contains(t, got[1].Note, "unavailable", "a failed section is a note and the task goes on")
	require.Contains(t, got[2].Note, "Attempt 2")
	require.Contains(t, got[2].Content, "too weak")
	require.Contains(t, got[3].Note, "no renderer")
}

func TestADiffOverTheLimitIsANoteWithItsSizeAndASmallOneIsInlineAndWrapped(t *testing.T) {
	s := Source{}
	s.Task.Inputs.Diff = ref
	small := Render(context.Background(), workflow.Kind{Inputs: []string{"diff"}}, s, deps(blobs{ref: "+ added line"}))
	require.Contains(t, small[0].Content, "[the diff of the pull request]+ added line")

	big := Render(context.Background(), workflow.Kind{Inputs: []string{"diff"}}, s, deps(blobs{ref: strings.Repeat("x", MaxInlineDiff+1)}))
	require.Empty(t, big[0].Content)
	require.Contains(t, big[0].Note, "read the files you need with read_code")
}

func TestTheTicketInputWrapsTheTicketTextAndEachCriterion(t *testing.T) {
	s := Source{}
	crit := workflow.BlobRef("blob:sha256:0000000000000000000000000000000000000000000000000000000000000002")
	s.Task.Inputs.AcceptanceCriteria = []workflow.CriterionRef{{ID: "AC1", Ref: crit}}
	s.Task.Inputs.Untrusted = []workflow.UntrustedRef{{Source: "ticket description", Ref: ref}}
	got := Render(context.Background(), workflow.Kind{Inputs: []string{"ticket"}}, s, deps(blobs{ref: "Refunds", crit: "A refund is capped"}))
	require.Contains(t, got[0].Content, "Refunds")
	require.Contains(t, got[0].Content, "AC1: A refund is capped")
	require.True(t, strings.HasPrefix(got[0].Content, "[the ticket and its acceptance criteria]"))
}

func TestARepoMapFoldsOtherTicketsTests(t *testing.T) {
	require.Equal(t, "a.go\ntests/heimdall/KAN-9/ (2 files of another ticket)",
		foldMap([]string{"a.go", "tests/heimdall/KAN-9/x_test.go", "tests/heimdall/KAN-9/y_test.go"}, "KAN-5"))
}

func TestAProvidedInputWithNoProviderOrNoTextIsAPlainNote(t *testing.T) {
	kind := workflow.Kind{Inputs: []string{"obligations"}}
	require.Contains(t, Render(context.Background(), kind, Source{}, deps(blobs{}))[0].Note, "not configured")
	d := deps(blobs{})
	d.Providers = map[string]Provider{"obligations": func(context.Context, Source) (string, error) { return "  ", nil }}
	require.Contains(t, Render(context.Background(), kind, Source{}, d)[0].Note, "Nothing to show")
}
