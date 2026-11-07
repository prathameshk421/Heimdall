package ports_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

type fakeTaskIndex struct {
	ref ports.ActiveTaskRef
	err error
}

var _ ports.TaskIndex = fakeTaskIndex{}

func (f fakeTaskIndex) ActiveTask(_ context.Context, _ id.AgentName, _ workflow.Role) (ports.ActiveTaskRef, error) {
	return f.ref, f.err
}

func TestTaskIndexContract(t *testing.T) {
	t.Parallel()
	fake := fakeTaskIndex{
		ref: ports.ActiveTaskRef{
			ChangeID: id.ChangeID("PAY-142"),
			TaskID:   id.TaskID("tsk_01HZX3K9QW7M2P5R8T0V4YB6CD"),
		},
	}
	ref, err := fake.ActiveTask(context.Background(), id.AgentName("bob"), workflow.RoleReviewer)
	require.NoError(t, err)
	require.Equal(t, id.ChangeID("PAY-142"), ref.ChangeID)
	require.Equal(t, id.TaskID("tsk_01HZX3K9QW7M2P5R8T0V4YB6CD"), ref.TaskID)
	require.NoError(t, ref.Validate())

	notFound := fakeTaskIndex{err: ports.ErrNotFound}
	_, err = notFound.ActiveTask(context.Background(), id.AgentName("alice"), workflow.RoleTestAuthor)
	require.ErrorIs(t, err, ports.ErrNotFound)
}

func TestActiveTaskRefValidation(t *testing.T) {
	t.Parallel()
	badChange := ports.ActiveTaskRef{
		ChangeID: id.ChangeID(""),
		TaskID:   id.TaskID("tsk_01HZX3K9QW7M2P5R8T0V4YB6CD"),
	}
	require.Error(t, badChange.Validate())

	badTask := ports.ActiveTaskRef{
		ChangeID: id.ChangeID("PAY-142"),
		TaskID:   id.TaskID("bad-task"),
	}
	require.Error(t, badTask.Validate())
}
