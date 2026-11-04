package file

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/tracker/internal/criteria"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/tracker/internal/poll"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

const ext = ".md"

// reTicketKey is the shape of a ticket key. It also keeps a key out of the file system: no separators, no dots.
var reTicketKey = regexp.MustCompile(`^[A-Z][A-Z0-9_]*-[0-9]+$`)

// Tracker reads ticket files from one directory (seed/tickets/<KEY>.md). It is safe for concurrent use.
type Tracker struct {
	dir   string
	clock ports.Clock
	seen  poll.Seen
}

// New returns a file tracker over dir. The directory is opened for each call, so a file added later is seen.
func New(dir string, clock ports.Clock) (*Tracker, error) {
	if dir == "" {
		return nil, errors.New("file tracker: a ticket directory is required")
	}
	if clock == nil {
		return nil, errors.New("file tracker: a clock is required")
	}
	return &Tracker{dir: dir, clock: clock}, nil
}

// Changes returns the ticket files modified inside the poll window. The changelog id is the SHA-256 of the file
// content, so a touch that keeps the content is not a change and an edit is. Pairs already reported inside the
// overlap are dropped (the same dedupe as the Jira poll).
func (t *Tracker) Changes(ctx context.Context, since ports.Cursor) ([]ports.TicketChange, ports.Cursor, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	now := t.clock.Now()
	start, err := poll.WindowStart(since, now)
	if err != nil {
		return nil, "", fmt.Errorf("file tracker changes: %w", err)
	}
	root, err := os.OpenRoot(t.dir)
	if err != nil {
		return nil, "", fmt.Errorf("file tracker: open ticket directory: %w", err)
	}
	defer func() { _ = root.Close() }()
	fsys := root.FS()
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, "", fmt.Errorf("file tracker: list ticket directory: %w", err)
	}
	var out []ports.TicketChange
	for _, e := range entries {
		change, err := t.changeFor(fsys, e, start)
		if err != nil {
			return nil, "", err
		}
		if change != nil {
			out = append(out, *change)
		}
	}
	t.seen.Forget(start)
	return out, poll.Format(now), nil
}

// changeFor returns the change for one directory entry, or nil when the entry is not a ticket file in the window
// or was already reported.
func (t *Tracker) changeFor(fsys fs.FS, e fs.DirEntry, start time.Time) (*ports.TicketChange, error) {
	key, ok := ticketKeyOf(e)
	if !ok {
		return nil, nil
	}
	info, err := e.Info()
	if err != nil {
		return nil, fmt.Errorf("file tracker: stat %s: %w", e.Name(), err)
	}
	if info.ModTime().Before(start) {
		return nil, nil
	}
	body, err := fs.ReadFile(fsys, e.Name())
	if err != nil {
		return nil, fmt.Errorf("file tracker: read %s: %w", e.Name(), err)
	}
	sum := sha256.Sum256(body)
	digest := hex.EncodeToString(sum[:])
	if !t.seen.Observe(key, digest, info.ModTime()) {
		return nil, nil
	}
	return &ports.TicketChange{
		Key:         key,
		ChangelogID: digest,
		Fields:      []string{"file"},
		At:          info.ModTime().UTC(),
	}, nil
}

// ticketKeyOf returns the ticket key named by a regular <KEY>.md entry.
func ticketKeyOf(e fs.DirEntry) (id.ChangeID, bool) {
	if !e.Type().IsRegular() || !strings.HasSuffix(e.Name(), ext) {
		return "", false
	}
	key := strings.TrimSuffix(e.Name(), ext)
	if !reTicketKey.MatchString(key) {
		return "", false
	}
	return id.ChangeID(key), true
}

// Ticket reads one ticket file. The title comes from the first heading ("# KEY: title"), whose key must match the
// file name. The Assignee and Status lines sit between that heading and the first "#" heading after it. The
// criteria come from the acceptance section of the whole file.
func (t *Tracker) Ticket(ctx context.Context, key id.ChangeID) (ports.Ticket, error) {
	if err := ctx.Err(); err != nil {
		return ports.Ticket{}, err
	}
	if !reTicketKey.MatchString(string(key)) {
		return ports.Ticket{}, fmt.Errorf("file tracker: %q is not a ticket key", string(key))
	}
	root, err := os.OpenRoot(t.dir)
	if err != nil {
		return ports.Ticket{}, fmt.Errorf("file tracker: open ticket directory: %w", err)
	}
	defer func() { _ = root.Close() }()
	body, err := fs.ReadFile(root.FS(), string(key)+ext)
	if errors.Is(err, fs.ErrNotExist) {
		return ports.Ticket{}, fmt.Errorf("file tracker: ticket %s: %w", string(key), ports.ErrNotFound)
	}
	if err != nil {
		return ports.Ticket{}, fmt.Errorf("file tracker: read ticket %s: %w", string(key), err)
	}
	tk, err := parseTicket(key, string(body))
	if err != nil {
		return ports.Ticket{}, fmt.Errorf("file tracker: ticket %s: %w", string(key), err)
	}
	if err := tk.Validate(); err != nil {
		return ports.Ticket{}, fmt.Errorf("file tracker: ticket %s: %w", string(key), err)
	}
	return tk, nil
}

// parseTicket reads the title, the metadata lines and the criteria of one ticket file.
func parseTicket(key id.ChangeID, body string) (ports.Ticket, error) {
	lines := strings.Split(body, "\n")
	first := -1
	for i, l := range lines {
		if strings.TrimSpace(l) != "" {
			first = i
			break
		}
	}
	if first < 0 {
		return ports.Ticket{}, errors.New("the file is empty")
	}
	title, err := headingTitle(key, lines[first])
	if err != nil {
		return ports.Ticket{}, err
	}
	assignee, status := metadata(lines[first+1:])
	cs, err := criteria.Parse(body)
	if err != nil {
		return ports.Ticket{}, err
	}
	return ports.Ticket{
		Key:      key,
		Title:    title,
		Assignee: change.ActorID(assignee),
		Status:   status,
		Criteria: cs,
	}, nil
}

// headingTitle returns the title in "# KEY: title" when KEY is the file's key.
func headingTitle(key id.ChangeID, line string) (string, error) {
	rest, ok := strings.CutPrefix(strings.TrimSpace(line), "# ")
	if !ok {
		return "", errors.New(`the first line must be "# KEY: title"`)
	}
	heading, title, ok := strings.Cut(rest, ": ")
	if !ok || strings.TrimSpace(heading) != string(key) {
		return "", fmt.Errorf("the heading key must be %s, the file name", string(key))
	}
	return strings.TrimSpace(title), nil
}

// metadata returns the Assignee and Status lines that appear before the next "#" heading.
func metadata(lines []string) (assignee, status string) {
	for _, l := range lines {
		line := strings.TrimSpace(l)
		if strings.HasPrefix(line, "#") {
			break
		}
		if v, ok := strings.CutPrefix(line, "Assignee:"); ok {
			assignee = strings.TrimSpace(v)
		}
		if v, ok := strings.CutPrefix(line, "Status:"); ok {
			status = strings.TrimSpace(v)
		}
	}
	return assignee, status
}
