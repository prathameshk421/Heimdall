package gitlog

import (
	"context"
	"errors"
	"fmt"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// snapshot is one verified view of a change at one tip of the ref. Every object it holds is addressed by hash, so the
// view is consistent even while other writers move the ref.
type snapshot struct {
	tip      id.SHA // commit the ref pointed at, "" when the ref does not exist yet
	rootTree id.SHA // tree of that commit, "" when the ref does not exist yet
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

// tree is the recursive listing of the state tree at one tip, by path.
type tree map[string]ports.GitTreeItem

// tip returns the commit the ref points at, or "" when the ref does not exist.
func (s *Store) tip(ctx context.Context) (id.SHA, error) {
	sha, err := s.git.GetRef(ctx, s.repo, s.ref)
	if errors.Is(err, ports.ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return sha, nil
}

// read returns the verified snapshot of one change at the current tip. When withEffects is set it also reads and
// verifies the effects file. Every verification failure is an IntegrityError.
func (s *Store) read(ctx context.Context, c id.ChangeID, withEffects bool) (*snapshot, error) {
	tip, err := s.tip(ctx)
	if err != nil {
		return nil, err
	}
	if tip == "" {
		return &snapshot{}, nil
	}
	commit, err := s.git.GetCommit(ctx, s.repo, tip)
	if err != nil {
		return nil, err
	}
	snap := &snapshot{tip: tip, rootTree: commit.Tree}
	t, err := s.listTree(ctx, commit.Tree)
	if err != nil {
		return nil, err
	}
	logSHA, headSHA, err := locateLog(c, t)
	if err != nil {
		return nil, err
	}
	if logSHA == "" && headSHA == "" {
		return snap, requireNoEffects(c, t, withEffects)
	}
	if err := s.loadLog(ctx, c, snap, logSHA, headSHA); err != nil {
		return nil, err
	}
	if withEffects {
		if err := s.loadEffects(ctx, c, snap, t); err != nil {
			return nil, err
		}
	}
	return snap, nil
}

// listTree reads the whole state tree in one recursive listing and indexes it by path.
func (s *Store) listTree(ctx context.Context, root id.SHA) (tree, error) {
	items, err := s.git.GetTree(ctx, s.repo, root, maxTreeEntries)
	if errors.Is(err, ports.ErrObjectTooLarge) {
		return nil, fmt.Errorf("state tree listing: %w: %w", ErrTooLarge, err)
	}
	if err != nil {
		return nil, err
	}
	t := make(tree, len(items))
	for _, it := range items {
		t[it.Path] = it
	}
	return t, nil
}

// fileIn returns the blob id of dir/name, or "" when it is absent. dir must be a directory if it exists, and the file
// must be a plain file.
func fileIn(c id.ChangeID, t tree, dir, name string) (id.SHA, error) {
	if d, ok := t[dir]; ok && d.Type != "tree" {
		return "", integrity(c, "state layout is wrong: "+dir+" is not a directory", nil)
	}
	e, ok := t[dir+"/"+name]
	if !ok {
		return "", nil
	}
	if e.Type != "blob" || e.Mode != "100644" {
		return "", integrity(c, "state layout is wrong: "+name+" is not a plain file", nil)
	}
	return e.SHA, nil
}

// locateLog returns the blob ids of the change file and its head file. A change has both or neither.
func locateLog(c id.ChangeID, t tree) (logSHA, headSHA id.SHA, err error) {
	if logSHA, err = fileIn(c, t, "changes", fileName(c, ".jsonl")); err != nil {
		return "", "", err
	}
	if headSHA, err = fileIn(c, t, "changes", fileName(c, ".head")); err != nil {
		return "", "", err
	}
	if (logSHA == "") != (headSHA == "") {
		return "", "", integrity(c, "the change file or its head file is missing", nil)
	}
	return logSHA, headSHA, nil
}

// blob reads one file of at most limit bytes and maps an oversized one to a LimitError.
func (s *Store) blob(ctx context.Context, sha id.SHA, what string, limit int64) ([]byte, error) {
	data, err := s.git.GetBlob(ctx, s.repo, sha, limit)
	if errors.Is(err, ports.ErrObjectTooLarge) {
		return nil, fmt.Errorf("%w: %w", &LimitError{What: what, Max: limit}, err)
	}
	return data, err
}

// loadLog reads the head, checks its MAC, then reads the log and checks the chain and the head against it. The MAC is
// checked first, so a file nobody holding the key signed is not parsed further.
func (s *Store) loadLog(ctx context.Context, c id.ChangeID, snap *snapshot, logSHA, headSHA id.SHA) error {
	headData, err := s.blob(ctx, headSHA, "head file", maxHeadBytes)
	if err != nil {
		return err
	}
	head, err := parseHead(s.key, c, headData)
	if err != nil {
		return err
	}
	data, err := s.blob(ctx, logSHA, "change file", MaxFileBytes)
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
func (s *Store) loadEffects(ctx context.Context, c id.ChangeID, snap *snapshot, t tree) error {
	sha, err := fileIn(c, t, "effects", fileName(c, ".jsonl"))
	if err != nil {
		return err
	}
	if sha != "" {
		if snap.effData, err = s.blob(ctx, sha, "effects file", MaxFileBytes); err != nil {
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
func requireNoEffects(c id.ChangeID, t tree, withEffects bool) error {
	if !withEffects {
		return nil
	}
	sha, err := fileIn(c, t, "effects", fileName(c, ".jsonl"))
	if err != nil {
		return err
	}
	if sha != "" {
		return integrity(c, "an effects file exists for a change with no log", nil)
	}
	return nil
}

func fileName(c id.ChangeID, ext string) string { return fmt.Sprintf("%s%s", c, ext) }
