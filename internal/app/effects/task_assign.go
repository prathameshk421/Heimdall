package effects

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// TaskAssignDeps are the dependencies of TaskAssign.
type TaskAssignDeps struct {
	Blobs   ports.BlobStore
	Chat    ports.Chat
	Inviter ports.RoomInviter
}

// TaskAssign performs task_assign effects: stores the bundle blob and posts a room notification.
type TaskAssign struct {
	deps TaskAssignDeps
}

// NewTaskAssign builds the executor, or refuses when a dependency is missing.
func NewTaskAssign(d TaskAssignDeps) (TaskAssign, error) {
	if d.Blobs == nil {
		return TaskAssign{}, fmt.Errorf("task_assign executor: missing blobs")
	}
	if d.Chat == nil {
		return TaskAssign{}, fmt.Errorf("task_assign executor: missing chat")
	}
	if d.Inviter == nil {
		return TaskAssign{}, fmt.Errorf("task_assign executor: missing inviter")
	}
	return TaskAssign{deps: d}, nil
}

// Execute stores the bundle blob under its SHA-256 and posts the room message.
func (t TaskAssign) Execute(ctx context.Context, fx change.Effect) error {
	if fx.Kind != change.EffectTaskAssign {
		return failf(nil, "task_assign executor: effect %s has kind %s", fx.Key, fx.Kind)
	}
	p, err := change.DecodeTaskAssignPayload(fx.Payload)
	if err != nil {
		return failf(err, "task_assign effect %s: payload refused", fx.Key)
	}

	sum := sha256.Sum256(p.Bundle)
	hashHex := hex.EncodeToString(sum[:])
	targetHash := strings.TrimPrefix(fx.Target, "blob:sha256:")
	if targetHash != hashHex {
		return failf(nil, "task_assign effect %s: hash mismatch: payload sha256 is %s, effect target is %s", fx.Key, hashHex, fx.Target)
	}

	_, err = t.deps.Blobs.Put(ctx, ports.BlobBundle, bytes.NewReader(p.Bundle))
	if err != nil {
		return failf(err, "task_assign %s: store bundle", fx.Key)
	}

	if err := t.deps.Inviter.InviteAgent(ctx, p.RoomID, string(p.Agent)); err != nil {
		return failf(err, "task_assign %s: invite %s into the room", fx.Key, p.Agent)
	}

	text := fmt.Sprintf("@%s you have a new task. Call heimdall_get_task.", p.Agent)
	err = t.deps.Chat.Post(ctx, p.RoomID, fx.Key, text)
	if err != nil {
		return failf(err, "task_assign %s: post room message", fx.Key)
	}

	return nil
}
