package reviewlearn

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// History lists the stored changes and loads the events of one.
type History interface {
	ChangeIDs(context.Context) ([]id.ChangeID, error)
	Load(context.Context, id.ChangeID) ([]change.Event, error)
}

// Service builds snapshots of review outcomes and caches them.
type Service struct {
	history History
	clock   ports.Clock
	ttl     time.Duration

	mu    sync.Mutex
	snap  *Snapshot
	built time.Time
}

// NewService returns a service. A snapshot is reused for ttl; a ttl of zero rebuilds on every call.
func NewService(h History, clock ports.Clock, ttl time.Duration) (*Service, error) {
	if h == nil || clock == nil {
		return nil, errors.New("review learning needs a history and a clock")
	}
	if ttl < 0 {
		return nil, errors.New("review learning ttl must not be negative")
	}
	return &Service{history: h, clock: clock, ttl: ttl}, nil
}

// Snapshot returns the outcomes of all stored changes. A cached snapshot younger than the ttl is returned as it is.
func (s *Service) Snapshot(ctx context.Context) (*Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.clock.Now()
	if s.snap != nil && s.ttl > 0 && now.Sub(s.built) < s.ttl {
		return s.snap, nil
	}
	snap, err := s.build(ctx)
	if err != nil {
		return nil, err
	}
	s.snap, s.built = snap, now
	return snap, nil
}

func (s *Service) build(ctx context.Context) (*Snapshot, error) {
	ids, err := s.history.ChangeIDs(ctx)
	if err != nil {
		return nil, fmt.Errorf("list changes for review learning: %w", err)
	}
	snap := &Snapshot{}
	for _, changeID := range ids {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		events, err := s.history.Load(ctx, changeID)
		if err != nil {
			return nil, fmt.Errorf("load change %s for review learning: %w", changeID, err)
		}
		folded := foldChange(changeID, events)
		snap.rows = append(snap.rows, folded.rows()...)
		snap.proposals = append(snap.proposals, folded.proposals...)
	}
	sortProposals(snap.proposals)
	return snap, nil
}
