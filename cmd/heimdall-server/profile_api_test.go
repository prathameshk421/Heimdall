package main

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

type logBlobs struct {
	ports.BlobStore
	logs map[workflow.BlobRef]string
}

func (b logBlobs) Get(_ context.Context, ref workflow.BlobRef) (io.ReadCloser, error) {
	text, ok := b.logs[ref]
	if !ok {
		return nil, ports.ErrNotFound
	}
	return io.NopCloser(bytes.NewBufferString(text)), nil
}

// A profiler asked for a new proposal after a failed trial sees that trial: outcome, counts and the log's end (D175).
func TestPreviousTrialShowsTheFailedTrialOfTheNewestProposal(t *testing.T) {
	ref := workflow.BlobRef("blob:sha256:" + strings.Repeat("d", 64))
	failed := ports.ProfileProposal{ID: strings.Repeat("c", 64), Repo: "acme/pay", State: ports.ProfileApproved,
		Trial: ports.ProfileTrial{Outcome: ports.TrialFailed, Passed: 2, Failed: 1, LogRef: ref}}
	api := profileAPI{store: latestProfiles{latest: &failed}, blobs: logBlobs{logs: map[workflow.BlobRef]string{ref: strings.Repeat("x", 20000) + "FAIL TestCap: go: command not found"}}}

	text, err := api.PreviousTrial(context.Background(), "acme/pay")
	require.NoError(t, err)
	require.Contains(t, text, "proposal "+failed.ID[:7]+" failed its trial: 2 passed, 1 failed, 0 errored")
	require.Contains(t, text, "FAIL TestCap: go: command not found", "the end of the log, where the failure is")
	require.Less(t, len(text), 17000, "the log is bounded")

	for _, p := range []ports.ProfileProposal{
		{ID: failed.ID, State: ports.ProfileProposed},
		{ID: failed.ID, State: ports.ProfileApproved, Trial: ports.ProfileTrial{Outcome: ports.TrialPassed}},
	} {
		text, err := profileAPI{store: latestProfiles{latest: &p}, blobs: logBlobs{}}.PreviousTrial(context.Background(), "acme/pay")
		require.NoError(t, err)
		require.Empty(t, text)
	}
	text, err = profileAPI{store: latestProfiles{}, blobs: logBlobs{}}.PreviousTrial(context.Background(), "acme/pay")
	require.NoError(t, err)
	require.Empty(t, text, "no proposal yet")
}
