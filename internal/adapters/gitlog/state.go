package gitlog

import (
	"context"
	"fmt"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

// snapshot is one verified view of a change at one tip of the ref. Every object it holds is addressed by hash, so the
// view is consistent even while other writers move the ref.
type snapshot struct {
	tip      string // commit the ref pointed at, "" when the ref does not exist yet
	rootTree string // tree of that commit, "" when the ref does not exist yet
	events   []change.Event
	log      []byte // the change file as read
	head     headFile
	effects  []effectLine // set only when the read asked for effects
	effData  []byte
}

// version is the number of the last event, or 0 for a change with no log.
func (s *snapshot) version() int64 { return int64(len(s.events)) }

// last is the hash of the last event, or the zero hash.
func (s *snapshot) last() change.Hash {
	if len(s.events) == 0 {
		return change.Hash{}
	}
	return s.events[len(s.events)-1].Hash
}

// read returns the verified snapshot of one change at the current tip. When withEffects is set it also reads and
// verifies the effects file. Every verification failure is an IntegrityError.
func (s *Store) read(ctx context.Context, c id.ChangeID, withEffects bool) (*snapshot, error) {
	tip, err := s.api.refSHA(ctx)
	if err != nil {
		return nil, err
	}
	if tip == "" {
		return &snapshot{}, nil
	}
	root, err := s.api.commitTree(ctx, tip)
	if err != nil {
		return nil, err
	}
	snap := &snapshot{tip: tip, rootTree: root}
	rootEntries, err := s.api.tree(ctx, root)
	if err != nil {
		return nil, err
	}
	logSHA, headSHA, err := s.locateLog(ctx, c, rootEntries)
	if err != nil {
		return nil, err
	}
	if logSHA == "" && headSHA == "" {
		return snap, s.requireNoEffects(ctx, c, rootEntries, withEffects)
	}
	if err := s.loadLog(ctx, c, snap, logSHA, headSHA); err != nil {
		return nil, err
	}
	if withEffects {
		if err := s.loadEffects(ctx, c, snap, rootEntries); err != nil {
			return nil, err
		}
	}
	return snap, nil
}

// dirEntries lists the directory name of a tree, or nil when it does not exist.
func (s *Store) dirEntries(ctx context.Context, c id.ChangeID, parent []treeEntry, name string) ([]treeEntry, error) {
	for _, e := range parent {
		if e.Path != name {
			continue
		}
		if e.Type != "tree" {
			return nil, integrity(c, "state layout is wrong: "+name+" is not a directory", nil)
		}
		return s.api.tree(ctx, e.SHA)
	}
	return nil, nil
}

// fileIn returns the blob id of a file in a listing, or "" when it is absent.
func fileIn(c id.ChangeID, entries []treeEntry, name string) (string, error) {
	for _, e := range entries {
		if e.Path == name {
			if e.Type != "blob" || e.Mode != "100644" {
				return "", integrity(c, "state layout is wrong: "+name+" is not a plain file", nil)
			}
			return e.SHA, nil
		}
	}
	return "", nil
}

// locateLog returns the blob ids of the change file and its head file. A change has both or neither.
func (s *Store) locateLog(ctx context.Context, c id.ChangeID, root []treeEntry) (logSHA, headSHA string, err error) {
	dir, err := s.dirEntries(ctx, c, root, "changes")
	if err != nil {
		return "", "", err
	}
	if logSHA, err = fileIn(c, dir, fileName(c, ".jsonl")); err != nil {
		return "", "", err
	}
	if headSHA, err = fileIn(c, dir, fileName(c, ".head")); err != nil {
		return "", "", err
	}
	if (logSHA == "") != (headSHA == "") {
		return "", "", integrity(c, "the change file or its head file is missing", nil)
	}
	return logSHA, headSHA, nil
}

// loadLog reads the head, checks its MAC, then reads the log and checks the chain and the head against it. The MAC is
// checked first, so a file nobody holding the key signed is not parsed further.
func (s *Store) loadLog(ctx context.Context, c id.ChangeID, snap *snapshot, logSHA, headSHA string) error {
	headData, err := s.api.blob(ctx, headSHA, "head file", maxHeadBytes)
	if err != nil {
		return err
	}
	head, err := parseHead(s.key, c, headData)
	if err != nil {
		return err
	}
	data, err := s.api.blob(ctx, logSHA, "change file", MaxFileBytes)
	if err != nil {
		return err
	}
	events, err := decodeEvents(c, data)
	if err != nil {
		return err
	}
	if err := change.VerifyChain(events); err != nil {
		return integrity(c, "hash chain is broken", err)
	}
	if len(events) == 0 || events[0].ChangeID != c {
		return integrity(c, "the file holds another change", nil)
	}
	if head.Version != int64(len(events)) || head.Hash != events[len(events)-1].Hash {
		return integrity(c, "head does not match the end of the log", nil)
	}
	snap.events, snap.log, snap.head = events, data, head
	return nil
}

// loadEffects reads the effects file and checks its digest against the signed head. A change with no effects file
// must have the digest of an empty file.
func (s *Store) loadEffects(ctx context.Context, c id.ChangeID, snap *snapshot, root []treeEntry) error {
	dir, err := s.dirEntries(ctx, c, root, "effects")
	if err != nil {
		return err
	}
	sha, err := fileIn(c, dir, fileName(c, ".jsonl"))
	if err != nil {
		return err
	}
	if sha != "" {
		if snap.effData, err = s.api.blob(ctx, sha, "effects file", MaxFileBytes); err != nil {
			return err
		}
	}
	want := emptyDigest()
	if snap.version() > 0 {
		want = snap.head.Effects
	}
	if digest(snap.effData) != want {
		return integrity(c, "effects file does not match the signed head", nil)
	}
	if snap.effects, err = decodeEffects(c, snap.effData); err != nil {
		return err
	}
	return nil
}

// requireNoEffects refuses an effects file that has no signed log behind it, when the caller will extend the change.
func (s *Store) requireNoEffects(ctx context.Context, c id.ChangeID, root []treeEntry, withEffects bool) error {
	if !withEffects {
		return nil
	}
	dir, err := s.dirEntries(ctx, c, root, "effects")
	if err != nil {
		return err
	}
	sha, err := fileIn(c, dir, fileName(c, ".jsonl"))
	if err != nil {
		return err
	}
	if sha != "" {
		return integrity(c, "an effects file exists for a change with no log", nil)
	}
	return nil
}

func fileName(c id.ChangeID, ext string) string { return fmt.Sprintf("%s%s", c, ext) }
