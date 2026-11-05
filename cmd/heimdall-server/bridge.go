package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/app/engine"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// starter opens a change with its first event. It adapts engine.Start, which also returns an outcome the poller
// does not use (trigger.Starter).
type starter struct {
	eng *engine.Engine
}

// Start opens the change. A change that already exists returns an error that wraps ports.ErrVersionConflict.
func (s starter) Start(ctx context.Context, first change.Event) error {
	_, err := s.eng.Start(ctx, first)
	return err
}

// ingestor hands the events of an executor to the engine (effects.Ingest). It adapts engine.Advance, which also
// returns an outcome the executors do not use.
type ingestor struct {
	eng *engine.Engine
}

// Ingest stores the events and runs the steps that follow them.
func (i ingestor) Ingest(ctx context.Context, changeID id.ChangeID, evs []change.Event) error {
	_, err := i.eng.Advance(ctx, changeID, evs)
	return err
}

// cursorTable is the cursor part of postgres.Store.
type cursorTable interface {
	GetCursor(ctx context.Context, source string) (string, error)
	SetCursor(ctx context.Context, source, value string) error
}

// cursorStore keeps the poll cursor of one tracker source in the cursor table (trigger.CursorStore). A source with no
// cursor yet loads as the empty cursor, so the first poll starts one overlap window back.
type cursorStore struct {
	table  cursorTable
	source string
}

// Load returns the saved cursor, or the empty cursor when none is saved.
func (c cursorStore) Load(ctx context.Context) (ports.Cursor, error) {
	value, err := c.table.GetCursor(ctx, c.source)
	if errors.Is(err, ports.ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("load cursor %s: %w", c.source, err)
	}
	return ports.Cursor(value), nil
}

// Save stores the cursor.
func (c cursorStore) Save(ctx context.Context, cur ports.Cursor) error {
	if err := c.table.SetCursor(ctx, c.source, string(cur)); err != nil {
		return fmt.Errorf("save cursor %s: %w", c.source, err)
	}
	return nil
}
