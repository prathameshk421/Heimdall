package gateuse

import (
	"context"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

// Request names the change whose outside facts are read.
type Request struct {
	ChangeID  id.ChangeID
	Repo      change.Repo
	Number    int
	HeadSHA   id.SHA
	Author    change.ActorID
	Approvers []string
}

// Source supplies the facts that are not in the event log (change.Outside).
type Source interface {
	Outside(ctx context.Context, req Request) (change.Outside, error)
}
