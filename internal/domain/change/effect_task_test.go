package change_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
	"github.com/stretchr/testify/require"
)

const (
	validTaskID = id.TaskID("tsk_01HZX3K9QW7M2P5R8T0V4YB6CD")
	validRoomID = change.RoomID("room_01HZX3K9QW7M2P5R8T0V4YB6CD")
	validAgent  = id.AgentName("bob-reviewer")
)

func validTaskAssignPayload() change.TaskAssignPayload {
	return change.TaskAssignPayload{
		Agent:  validAgent,
		Role:   workflow.RoleReviewer,
		TaskID: validTaskID,
		RoomID: validRoomID,
		Bundle: []byte(`{"ticket":"PAY-1"}`),
	}
}

func TestEffectTaskAssignKindValid(t *testing.T) {
	require.True(t, change.EffectTaskAssign.Valid())
	require.Equal(t, change.EffectTaskAssign, change.EffectKind("task_assign"))
}

func TestTaskAssignPayloadValidate(t *testing.T) {
	t.Run("valid payload passes", func(t *testing.T) {
		p := validTaskAssignPayload()
		require.NoError(t, p.Validate())
	})

	t.Run("empty agent", func(t *testing.T) {
		p := validTaskAssignPayload()
		p.Agent = ""
		require.Error(t, p.Validate())
	})

	t.Run("invalid agent format", func(t *testing.T) {
		p := validTaskAssignPayload()
		p.Agent = "INVALID AGENT"
		require.Error(t, p.Validate())
	})

	t.Run("invalid role", func(t *testing.T) {
		p := validTaskAssignPayload()
		p.Role = "wizard"
		require.Error(t, p.Validate())
	})

	t.Run("invalid task id", func(t *testing.T) {
		p := validTaskAssignPayload()
		p.TaskID = "not-a-task-id"
		require.Error(t, p.Validate())
	})

	t.Run("invalid room id", func(t *testing.T) {
		p := validTaskAssignPayload()
		p.RoomID = ""
		require.Error(t, p.Validate())
	})

	t.Run("empty bundle", func(t *testing.T) {
		p := validTaskAssignPayload()
		p.Bundle = nil
		require.Error(t, p.Validate())

		p.Bundle = []byte{}
		require.Error(t, p.Validate())
	})

	t.Run("bundle exceeds max size", func(t *testing.T) {
		p := validTaskAssignPayload()
		p.Bundle = make([]byte, change.MaxBundleBytes+1)
		require.Error(t, p.Validate())
	})
}

func TestDecodeTaskAssignPayloadStrict(t *testing.T) {
	p := validTaskAssignPayload()
	bundleB64 := base64.StdEncoding.EncodeToString(p.Bundle)

	validJSON := func() map[string]any {
		return map[string]any{
			"agent":   string(p.Agent),
			"role":    string(p.Role),
			"task_id": string(p.TaskID),
			"room_id": string(p.RoomID),
			"bundle":  bundleB64,
		}
	}

	t.Run("valid JSON decodes successfully", func(t *testing.T) {
		data, err := json.Marshal(validJSON())
		require.NoError(t, err)

		decoded, err := change.DecodeTaskAssignPayload(data)
		require.NoError(t, err)
		require.Equal(t, p.Agent, decoded.Agent)
		require.Equal(t, p.Role, decoded.Role)
		require.Equal(t, p.TaskID, decoded.TaskID)
		require.Equal(t, p.RoomID, decoded.RoomID)
		require.Equal(t, p.Bundle, decoded.Bundle)
	})

	t.Run("not a JSON object", func(t *testing.T) {
		_, err := change.DecodeTaskAssignPayload(json.RawMessage(`"just a string"`))
		require.Error(t, err)
	})

	t.Run("null is refused", func(t *testing.T) {
		_, err := change.DecodeTaskAssignPayload(json.RawMessage(`null`))
		require.Error(t, err)
	})

	for _, field := range []string{"agent", "role", "task_id", "room_id", "bundle"} {
		t.Run("missing required field "+field, func(t *testing.T) {
			m := validJSON()
			delete(m, field)
			data, err := json.Marshal(m)
			require.NoError(t, err)
			_, err = change.DecodeTaskAssignPayload(data)
			require.Error(t, err)
			require.Contains(t, err.Error(), `missing field "`+field+`"`)
		})
	}

	t.Run("unknown fields refused", func(t *testing.T) {
		m := validJSON()
		m["extra_field"] = "sneaky"
		data, err := json.Marshal(m)
		require.NoError(t, err)
		_, err = change.DecodeTaskAssignPayload(data)
		require.Error(t, err)
	})

	t.Run("data after object refused", func(t *testing.T) {
		data, err := json.Marshal(validJSON())
		require.NoError(t, err)
		withTrailing := append(bytes.Clone(data), []byte("   {}")...)
		_, err = change.DecodeTaskAssignPayload(withTrailing)
		require.Error(t, err)
	})

	t.Run("invalid base64 in bundle", func(t *testing.T) {
		m := validJSON()
		m["bundle"] = "not_valid_base64!@#"
		data, err := json.Marshal(m)
		require.NoError(t, err)
		_, err = change.DecodeTaskAssignPayload(data)
		require.Error(t, err)
	})
}
