package gitlog

import (
	"context"
	"errors"
	"fmt"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// Reset replaces a change's log with a fresh one whose first event is state.reset, and moves the ref by
// compare-and-swap (ACTION_MODE 2.7). It is the one repair of a log that failed its MAC or its chain: the caller has
// already decided the old log cannot be believed, so this method never reads its events, only the tree.
//
// The new log is exactly evs, chained from nothing. An effects file the change had is replaced with an empty one, so
// the signed digest of an empty file matches the new head and a later Append can extend the log.
func (s *Store) Reset(ctx context.Context, changeID id.ChangeID, evs []change.Event) error {
	if err := resetBatchValid(changeID, evs); err != nil {
		return fmt.Errorf("reset %s: %w", changeID, err)
	}
	chained, err := chainEvents(evs, change.Hash{}, true)
	if err != nil {
		return fmt.Errorf("reset %s: %w", changeID, err)
	}
	logData := make([]byte, 0, len(chained)*128)
	for _, e := range chained {
		line, err := encodeEvent(e)
		if err != nil {
			return fmt.Errorf("reset %s: %w", changeID, err)
		}
		logData = append(logData, line...)
	}
	head, err := signHead(s.key, changeID, int64(len(chained)), chained[len(chained)-1].Hash, emptyDigest())
	if err != nil {
		return fmt.Errorf("reset %s: %w", changeID, err)
	}
	for range maxRebuilds {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("reset %s: %w", changeID, err)
		}
		snap, cleared, err := s.resetPlan(ctx, changeID)
		if err != nil {
			return fmt.Errorf("reset %s: %w", changeID, err)
		}
		files := cleared
		files = append(files,
			file{path: "changes/" + fileName(changeID, ".jsonl"), content: logData},
			file{path: "changes/" + fileName(changeID, ".head"), content: head})
		err = s.publish(ctx, snap, &writePlan{files: files, version: int64(len(chained))},
			fmt.Sprintf("heimdall state: %s reset", changeID))
		if err == nil {
			return nil
		}
		if !errors.Is(err, errRefMoved) {
			return fmt.Errorf("reset %s: %w", changeID, err)
		}
	}
	return fmt.Errorf("reset %s: the ref moved %d times in a row: %w", changeID, maxRebuilds, ports.ErrUnavailable)
}

// resetBatchValid checks a reset batch with no call: it must not be empty, it must start with state.reset, and every
// event must be valid at its version. The MAC key never enters an error.
func resetBatchValid(changeID id.ChangeID, evs []change.Event) error {
	if _, err := id.ParseChangeID(string(changeID)); err != nil {
		return err
	}
	if len(evs) == 0 {
		return errors.New("a reset needs at least the state.reset event")
	}
	if evs[0].Type != change.EventStateReset {
		return fmt.Errorf("the first event is %s, not %s", evs[0].Type, change.EventStateReset)
	}
	prepared, err := prepare(changeID, 0, evs, nil)
	if err != nil {
		return err
	}
	return checkFreshIDs(nil, prepared)
}

// resetPlan reads the tree at the current tip and returns the files that must be replaced besides the new log: an
// effects file, set to empty so the new head's digest matches. A ref that does not exist yet gives an empty snapshot.
func (s *Store) resetPlan(ctx context.Context, changeID id.ChangeID) (*snapshot, []file, error) {
	snap := &snapshot{}
	tip, err := readTip(ctx, s.git, s.repo, s.ref)
	if err != nil || tip == "" {
		return snap, nil, err
	}
	commit, err := s.git.GetCommit(ctx, s.repo, tip)
	if err != nil {
		return nil, nil, err
	}
	snap.tip, snap.rootTree = tip, commit.Tree
	t, err := s.listTree(ctx, snap.rootTree)
	if err != nil {
		return nil, nil, err
	}
	if d, ok := t["effects"]; ok && d.Type != "tree" {
		return nil, nil, errors.New("the effects entry is not a directory")
	}
	effectsPath := "effects/" + fileName(changeID, ".jsonl")
	if _, ok := t[effectsPath]; !ok {
		return snap, nil, nil
	}
	return snap, []file{{path: effectsPath, content: []byte{}}}, nil
}
