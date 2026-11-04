package poll

import (
	"fmt"
	"sync"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// Overlap is how far before the cursor every poll starts (TECH_SPEC 13, D97). A change the search index shows
// late is still inside the next poll's window.
const Overlap = 10 * time.Minute

// WindowStart returns the instant a poll starts reading from. An empty cursor (the first poll) starts one overlap
// before now, so a fresh process does not look back without bound.
func WindowStart(since ports.Cursor, now time.Time) (time.Time, error) {
	if since == "" {
		return now.Add(-Overlap), nil
	}
	t, err := time.Parse(time.RFC3339Nano, string(since))
	if err != nil {
		return time.Time{}, fmt.Errorf("poll cursor %q: %w", string(since), err)
	}
	return t.Add(-Overlap), nil
}

// Format renders a poll time as the cursor the next poll receives.
func Format(t time.Time) ports.Cursor {
	return ports.Cursor(t.UTC().Format(time.RFC3339Nano))
}

type seenKey struct {
	key       id.ChangeID
	changelog string
}

// Seen remembers which (ticket key, changelog id) pairs a poll already reported, so a repeat inside the overlap
// window triggers nothing twice. The zero value is ready to use. It is safe for concurrent use.
type Seen struct {
	mu sync.Mutex
	at map[seenKey]time.Time
}

// Observe records a pair and reports true when it is new. A repeat returns false and keeps its first time.
func (s *Seen) Observe(key id.ChangeID, changelogID string, at time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.at == nil {
		s.at = make(map[seenKey]time.Time)
	}
	k := seenKey{key: key, changelog: changelogID}
	if _, ok := s.at[k]; ok {
		return false
	}
	s.at[k] = at
	return true
}

// Forget drops pairs whose time is before the window start. A poll never reads before its window, so a dropped pair
// cannot come back as a repeat.
func (s *Seen) Forget(before time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, at := range s.at {
		if at.Before(before) {
			delete(s.at, k)
		}
	}
}
