package change_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
)

// Found live in H11: PR #16 was closed to stop a paid loop and reopened later, and the change stayed over for good.
func TestAReopenedPullRequestResumesTheChange(t *testing.T) {
	s := newStream(t).
		add(change.EventPROpened, githubActor, change.PROpened{Repo: prRepo, Number: prNumber, HeadSHA: head, Author: author}).
		add(change.EventPRClosed, githubActor, change.PRClosed{Repo: prRepo, Number: prNumber, HeadSHA: head}).
		add(change.EventPRReopened, githubActor, change.PRReopened{Repo: prRepo, Number: prNumber, HeadSHA: head})
	v, err := change.Fold(s.events())
	require.NoError(t, err)
	require.False(t, v.Over(), "a reopened pull request is open again")
	require.NotNil(t, v.PR)
	require.Equal(t, head, v.PR.HeadSHA)

	s.add(change.EventPRClosed, githubActor, change.PRClosed{Repo: prRepo, Number: prNumber, HeadSHA: head})
	v, err = change.Fold(s.events())
	require.NoError(t, err)
	require.True(t, v.Over(), "a second close ends the change again")
}

func TestAReopenDoesNotUndoAMergeOrAnotherPullRequest(t *testing.T) {
	merged := newStream(t).
		add(change.EventPROpened, githubActor, change.PROpened{Repo: prRepo, Number: prNumber, HeadSHA: head, Author: author}).
		add(change.EventPRClosed, githubActor, change.PRClosed{Repo: prRepo, Number: prNumber, HeadSHA: head, Merged: true}).
		add(change.EventPRReopened, githubActor, change.PRReopened{Repo: prRepo, Number: prNumber, HeadSHA: head})
	v, err := change.Fold(merged.events())
	require.NoError(t, err)
	require.True(t, v.Over(), "a merged change stays merged")

	other := newStream(t).
		add(change.EventPROpened, githubActor, change.PROpened{Repo: prRepo, Number: prNumber, HeadSHA: head, Author: author}).
		add(change.EventPRClosed, githubActor, change.PRClosed{Repo: prRepo, Number: prNumber, HeadSHA: head}).
		add(change.EventPRReopened, githubActor, change.PRReopened{Repo: prRepo, Number: prNumber + 1, HeadSHA: head})
	v, err = change.Fold(other.events())
	require.NoError(t, err)
	require.True(t, v.Over(), "only the change's own pull request can reopen it")
}

func TestAReopenNeedsARepoANumberAndAHead(t *testing.T) {
	require.Error(t, change.PRReopened{}.Validate())
	require.NoError(t, change.PRReopened{Repo: prRepo, Number: prNumber, HeadSHA: head}.Validate())
}
