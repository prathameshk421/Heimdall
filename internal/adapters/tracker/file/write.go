package file

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

var (
	_ ports.TicketWriter       = (*Tracker)(nil)
	_ ports.TicketTransitioner = (*Tracker)(nil)
)

// Comment appends a comment to the ticket's markdown file. An existing marker for key makes a repeat a no-op.
func (t *Tracker) Comment(ctx context.Context, ticket id.ChangeID, key change.EffectKey, text string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !reTicketKey.MatchString(string(ticket)) {
		return fmt.Errorf("file tracker comment: %q is not a ticket key", string(ticket))
	}
	if key == "" {
		return errors.New("file tracker comment: effect key is required")
	}
	root, err := os.OpenRoot(t.dir)
	if err != nil {
		return fmt.Errorf("file tracker: open ticket directory: %w", err)
	}
	defer func() { _ = root.Close() }()
	name := string(ticket) + ext
	body, err := fs.ReadFile(root.FS(), name)
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("file tracker: ticket %s: %w", ticket, ports.ErrNotFound)
	}
	if err != nil {
		return fmt.Errorf("file tracker: read ticket %s: %w", ticket, err)
	}
	marker := "<!-- heimdall:" + string(key) + " -->"
	if strings.Contains(string(body), marker) {
		return nil
	}
	next := strings.TrimRight(string(body), "\n") + "\n\n" + marker + "\n" + text + "\n"
	if err := root.WriteFile(name, []byte(next), 0o600); err != nil {
		return fmt.Errorf("file tracker: write ticket %s: %w", ticket, err)
	}
	return nil
}

// Transition updates the Status metadata line of the ticket's markdown file.
func (t *Tracker) Transition(ctx context.Context, ticket id.ChangeID, _ change.EffectKey, status string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !reTicketKey.MatchString(string(ticket)) {
		return fmt.Errorf("file tracker transition: %q is not a ticket key", string(ticket))
	}
	root, err := os.OpenRoot(t.dir)
	if err != nil {
		return fmt.Errorf("file tracker: open ticket directory: %w", err)
	}
	defer func() { _ = root.Close() }()
	name := string(ticket) + ext
	body, err := fs.ReadFile(root.FS(), name)
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("file tracker: ticket %s: %w", ticket, ports.ErrNotFound)
	}
	if err != nil {
		return fmt.Errorf("file tracker: read ticket %s: %w", ticket, err)
	}
	lines := strings.Split(string(body), "\n")
	replaced := false
	for i, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), "Status:") {
			lines[i] = "Status: " + status
			replaced = true
			break
		}
	}
	if !replaced {
		lines = append(lines, "Status: "+status)
	}
	if err := root.WriteFile(name, []byte(strings.Join(lines, "\n")), 0o600); err != nil {
		return fmt.Errorf("file tracker: write ticket %s: %w", ticket, err)
	}
	return nil
}

// CreateTicket writes a new ticket markdown file in the ticket directory.
func (t *Tracker) CreateTicket(ctx context.Context, d ports.TicketDraft) (id.ChangeID, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := d.Validate(); err != nil {
		return "", fmt.Errorf("file tracker create ticket: %w", err)
	}
	root, err := os.OpenRoot(t.dir)
	if err != nil {
		return "", fmt.Errorf("file tracker: open ticket directory: %w", err)
	}
	defer func() { _ = root.Close() }()
	entries, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return "", fmt.Errorf("file tracker: list ticket directory: %w", err)
	}
	key := id.ChangeID(fmt.Sprintf("LOCAL-%d", len(entries)+1))
	content := fmt.Sprintf("# %s: %s\nAssignee: local\nStatus: Open\n\n%s\n", key, d.Title, d.Body)
	if err := root.WriteFile(string(key)+ext, []byte(content), 0o600); err != nil {
		return "", fmt.Errorf("file tracker: write ticket %s: %w", key, err)
	}
	return key, nil
}
