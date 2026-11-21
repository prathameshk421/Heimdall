package gitlog

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// errRefMoved says the ref update was refused because another writer moved the ref. Append reads again.
var errRefMoved = errors.New("ref moved")

// file is one path of the new commit.
type file struct {
	path    string
	content []byte
}

// writePlan is the set of files of one append and the version it reaches.
type writePlan struct {
	files   []file
	version int64
}

// plan chains evs after the stored head and builds the files that change: the change file, its head, and the effects
// file when it gets a new row. A row whose key is stored with the same content is skipped. One with other content is
// ErrEffectKeyReused.
func (s *Store) plan(c id.ChangeID, snap *snapshot, evs []change.Event, fx []change.Effect) (*writePlan, error) {
	if err := checkFreshIDs(snap.events, evs); err != nil {
		return nil, err
	}
	chained, err := chainEvents(evs, snap.last(), snap.version() == 0)
	if err != nil {
		return nil, err
	}
	logData, err := appendEvents(snap.log, chained)
	if err != nil {
		return nil, err
	}
	lastEvent := chained[len(chained)-1]
	files := []file{{path: "changes/" + fileName(c, ".jsonl"), content: logData}}
	effects := emptyDigest()
	if snap.version() > 0 {
		effects = snap.head.Effects
	}
	rows, err := newEffectRows(snap, lastEvent.ID, fx)
	if err != nil {
		return nil, err
	}
	if len(rows) > 0 {
		effData := append(bytes.Clone(snap.effData), rows...)
		if len(effData) > MaxFileBytes {
			return nil, &LimitError{What: "effects file", Max: MaxFileBytes, Got: int64(len(effData))}
		}
		effects = digest(effData)
		files = append(files, file{path: "effects/" + fileName(c, ".jsonl"), content: effData})
	}
	version := snap.version() + int64(len(chained))
	head, err := signHead(s.key, c, version, lastEvent.Hash, effects)
	if err != nil {
		return nil, err
	}
	files = append(files, file{path: "changes/" + fileName(c, ".head"), content: head})
	return &writePlan{files: files, version: version}, nil
}

// checkFreshIDs refuses an event id that is already in the log or twice in the batch. Postgres does the same with
// its primary key.
func checkFreshIDs(stored, evs []change.Event) error {
	seen := make(map[change.EventID]struct{}, len(stored)+len(evs))
	for _, e := range stored {
		seen[e.ID] = struct{}{}
	}
	for _, e := range evs {
		if _, dup := seen[e.ID]; dup {
			return fmt.Errorf("event id %s is already used in this change", e.ID)
		}
		seen[e.ID] = struct{}{}
	}
	return nil
}

// appendEvents returns the change file after adding evs, within the file limit.
func appendEvents(old []byte, evs []change.Event) ([]byte, error) {
	data := bytes.Clone(old)
	for _, e := range evs {
		line, err := encodeEvent(e)
		if err != nil {
			return nil, err
		}
		data = append(data, line...)
	}
	if len(data) > MaxFileBytes {
		return nil, &LimitError{What: "change file", Max: MaxFileBytes, Got: int64(len(data))}
	}
	return data, nil
}

// chainEvents sets PrevHash and Hash on each event, starting after prev. A genesis batch has no previous hash.
func chainEvents(evs []change.Event, prev change.Hash, first bool) ([]change.Event, error) {
	out := make([]change.Event, len(evs))
	for i, e := range evs {
		if i == 0 && first {
			e.PrevHash = nil
		} else {
			p := prev
			e.PrevHash = &p
		}
		h, err := change.Chain(prev, e)
		if err != nil {
			return nil, fmt.Errorf("hash version %d: %w", e.Version, err)
		}
		e.Hash = h
		prev = h
		out[i] = e
	}
	return out, nil
}

// newEffectRows returns the JSONL rows to add for fx, caused by the event with id cause.
func newEffectRows(snap *snapshot, cause change.EventID, fx []change.Effect) ([]byte, error) {
	known := make(map[change.EffectKey]change.Effect, len(snap.effects)+len(fx))
	for _, l := range snap.effects {
		known[l.Effect.Key] = l.Effect
	}
	var rows []byte
	for _, f := range fx {
		if old, ok := known[f.Key]; ok {
			if !sameEffect(old, f) {
				return nil, fmt.Errorf("key %s: %w", f.Key, ErrEffectKeyReused)
			}
			continue
		}
		known[f.Key] = f
		line, err := json.Marshal(effectLine{EventID: cause, Effect: f})
		if err != nil {
			return nil, fmt.Errorf("encode effect %s: %w", f.Key, err)
		}
		if len(line) > maxEffectBytes {
			return nil, &LimitError{What: "effect " + string(f.Key), Max: maxEffectBytes, Got: int64(len(line))}
		}
		rows = append(append(rows, line...), '\n')
	}
	return rows, nil
}

// publish makes the blobs, the tree and the commit, then moves the ref to it. It returns nil when the commit is the
// tip, errRefMoved when another writer moved the ref first, or another error.
func (s *Store) publish(ctx context.Context, snap *snapshot, p *writePlan, message string) error {
	entries := make([]ports.GitTreeEntry, 0, len(p.files))
	for _, f := range p.files {
		sha, err := s.git.CreateBlob(ctx, s.repo, f.content)
		if err != nil {
			return err
		}
		entries = append(entries, ports.GitTreeEntry{Path: f.path, Mode: "100644", SHA: sha})
	}
	tree, err := s.git.CreateTree(ctx, s.repo, snap.rootTree, entries)
	if err != nil {
		return err
	}
	var parents []id.SHA
	if snap.tip != "" {
		parents = []id.SHA{snap.tip}
	}
	commit, err := s.git.CreateCommit(ctx, s.repo, message, tree, parents)
	if err != nil {
		return err
	}
	return s.moveRef(ctx, snap.tip, commit)
}

// moveRef moves the ref from before ("" when it does not exist) to after, and learns the outcome of every refusal by
// reading the tip again (S1 1.7 point 5). GitHub does not say why it refused an updateRefs, and a lost reply looks
// the same, so the tip decides:
//
//   - tip == after: the move landed and only the answer was lost. Success.
//   - tip == before: nothing moved. A transient failure: wait (1 s, 2 s, 4 s) and offer the same commit again, at most
//     maxSwapAttempts times, then ports.ErrUnavailable.
//   - any other tip: another writer won. errRefMoved, and Append reads again.
//
// ports.ErrUnauthorized (FORBIDDEN: the token has no contents: write) is returned at once. The store fails closed.
func (s *Store) moveRef(ctx context.Context, before, after id.SHA) error {
	for attempt := 1; ; attempt++ {
		var err error
		if before == "" {
			err = s.git.CreateRef(ctx, s.repo, s.ref, after)
		} else {
			err = s.git.SwapRef(ctx, s.repo, s.ref, before, after)
		}
		if err == nil {
			return nil
		}
		if errors.Is(err, ports.ErrUnauthorized) {
			return fmt.Errorf("move %s: %w", s.ref, err)
		}
		if cerr := ctx.Err(); cerr != nil {
			return fmt.Errorf("move %s: outcome unknown: %w", s.ref, cerr)
		}
		tip, rerr := s.tip(ctx)
		if rerr != nil {
			return fmt.Errorf("move %s: outcome unknown, the tip could not be read again: %w", s.ref, errors.Join(err, rerr))
		}
		switch tip {
		case after:
			return nil
		case before:
			if attempt >= maxSwapAttempts {
				return fmt.Errorf("move %s: refused %d times while the ref did not move: %w: %w", s.ref, attempt, ports.ErrUnavailable, err)
			}
			if werr := s.wait(ctx, swapBackoff<<(attempt-1)); werr != nil {
				return fmt.Errorf("move %s: waiting to try again: %w", s.ref, werr)
			}
		default:
			return errRefMoved
		}
	}
}
