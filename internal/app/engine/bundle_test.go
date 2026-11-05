package engine_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/engine"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/stretchr/testify/require"
)

func makeTestEvent(t *testing.T, typ change.EventType, payload change.Payload) change.Event {
	t.Helper()
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	return change.Event{
		ID:         change.EventID("evt_01HZX3K9QW7M2P5R8T0V4YB6CD"),
		ChangeID:   id.ChangeID("PAY-142"),
		Version:    1,
		Type:       typ,
		Actor:      change.Actor{Kind: change.ActorSystem, ID: "heimdall"},
		OccurredAt: time.Date(2026, 11, 5, 14, 0, 0, 0, time.UTC),
		Payload:    raw,
	}
}

func TestBuildBundle(t *testing.T) {
	ticket := id.ChangeID("PAY-142")
	repo := change.Repo("acme/pay-demo")
	assignee := change.ActorID("ana")
	baseSHA := id.SHA("a01b2c3d4e5f60718293a4b5c6d7e8f9a0b1c2d3")
	headSHA := id.SHA("9f1c2d3e4b5a6c7d8e9f0a1b2c3d4e5f6a7b8c9d")

	trigEv := makeTestEvent(t, change.EventChangeTriggered, change.Triggered{
		Ticket:   ticket,
		Repo:     repo,
		Assignee: assignee,
		Source:   change.TriggerFile,
	})

	branchEv := makeTestEvent(t, change.EventBranchEnsured, change.BranchEnsured{
		Repo:    repo,
		Branch:  "PAY-142-test",
		BaseSHA: baseSHA,
	})

	prEv := makeTestEvent(t, change.EventPROpened, change.PROpened{
		Repo:    repo,
		Number:  42,
		HeadSHA: headSHA,
		Author:  assignee,
		Ticket:  ticket,
	})

	view := engine.ChangeView{
		ID:     ticket,
		Events: []change.Event{trigEv, branchEv, prEv},
	}

	t.Run("determinism: same view gives the exact same bundle bytes and hash twice", func(t *testing.T) {
		bytes1, ref1, err := engine.BuildBundle(view)
		require.NoError(t, err)
		require.NotEmpty(t, bytes1)

		bytes2, ref2, err := engine.BuildBundle(view)
		require.NoError(t, err)

		require.Equal(t, bytes1, bytes2)
		require.Equal(t, ref1, ref2)

		// Check SHA256 matches ref
		sum := sha256.Sum256(bytes1)
		expectedRef := fmt.Sprintf("blob:sha256:%s", hex.EncodeToString(sum[:]))
		require.Equal(t, expectedRef, string(ref1))

		// Check unmarshalled content
		var bundle engine.ContextBundle
		err = json.Unmarshal(bytes1, &bundle)
		require.NoError(t, err)
		require.Equal(t, string(ticket), bundle.Ticket)
		require.Equal(t, string(repo), bundle.Repo)
		require.Equal(t, string(assignee), bundle.Assignee)
		require.Equal(t, string(baseSHA), bundle.BaseSHA)
		require.Equal(t, string(headSHA), bundle.HeadSHA)
	})

	t.Run("with acceptance criteria from tests.submitted", func(t *testing.T) {
		testsEv := makeTestEvent(t, change.EventTestsSubmitted, change.TestsSubmitted{
			Refs: []string{"test1"},
			ACMap: map[string][]string{
				"AC9": {"Test9"},
				"AC2": {"Test2"},
				"AC1": {"Test1"},
				"AC5": {"Test5"},
				"AC3": {"Test3"},
				"AC8": {"Test8"},
				"AC4": {"Test4"},
				"AC7": {"Test7"},
				"AC6": {"Test6"},
			},
			SHA:      headSHA,
			TreeHash: id.SHA("1111222233334444555566667777888899990000"),
		})

		viewWithAC := engine.ChangeView{
			ID:     ticket,
			Events: []change.Event{trigEv, branchEv, prEv, testsEv},
		}

		for iter := 0; iter < 10; iter++ {
			bundleBytes, _, err := engine.BuildBundle(viewWithAC)
			require.NoError(t, err)

			var bundle engine.ContextBundle
			err = json.Unmarshal(bundleBytes, &bundle)
			require.NoError(t, err)
			require.Equal(t, []string{"AC1", "AC2", "AC3", "AC4", "AC5", "AC6", "AC7", "AC8", "AC9"}, bundle.AcceptanceCriteria)
		}
	})
}
