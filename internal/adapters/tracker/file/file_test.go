package file

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/tracker/internal/poll"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

var now = time.Date(2026, 11, 4, 10, 0, 0, 0, time.UTC)

// fixedClock is a ports.Clock a test moves between polls.
type fixedClock struct{ at time.Time }

func (c *fixedClock) Now() time.Time { return c.at }

const pay142 = `# PAY-142: Add refunds

Assignee: ana
Status: In Progress

Merchants can refund a payment, in full or in part.

## Acceptance criteria

1. A refund cannot exceed the original payment.
2. A repeated request with the same ` + "`Idempotency-Key`" + ` returns the original refund.
`

func digest(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// write puts a ticket file in dir and sets its modification time.
func write(t *testing.T, dir, name, content string, mtime time.Time) {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	require.NoError(t, os.Chtimes(path, mtime, mtime))
}

func newTracker(t *testing.T, dir string, at time.Time) *Tracker {
	t.Helper()
	tr, err := New(dir, &fixedClock{at: at})
	require.NoError(t, err)
	return tr
}

func TestTicketReadsTitleAssigneeStatusAndCriteria(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "PAY-142.md", pay142, now)

	got, err := newTracker(t, dir, now).Ticket(context.Background(), "PAY-142")

	require.NoError(t, err)
	require.Equal(t, "PAY-142", string(got.Key))
	require.Equal(t, "Add refunds", got.Title)
	require.Equal(t, "ana", string(got.Assignee))
	require.Equal(t, "In Progress", got.Status)
	require.Len(t, got.Criteria, 2)
	require.Equal(t, "AC1", got.Criteria[0].ID)
	require.Equal(t, "A refund cannot exceed the original payment.", got.Criteria[0].Text)
	require.Equal(t, digest(got.Criteria[0].Text), got.Criteria[0].Hash)
	require.NoError(t, got.Validate())
}

func TestAMissingTicketFileIsNotFound(t *testing.T) {
	_, err := newTracker(t, t.TempDir(), now).Ticket(context.Background(), "PAY-404")

	require.ErrorIs(t, err, ports.ErrNotFound)
}

func TestTheHeadingKeyMustMatchTheFileName(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "PAY-142.md", "# PAY-999: wrong\n\nAssignee: ana\nStatus: In Progress\n", now)

	_, err := newTracker(t, dir, now).Ticket(context.Background(), "PAY-142")

	require.Error(t, err)
}

func TestATicketWithoutAnAssigneeIsRefused(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "PAY-142.md", "# PAY-142: No owner\n\nStatus: In Progress\n\n## Acceptance criteria\n- x\n", now)

	_, err := newTracker(t, dir, now).Ticket(context.Background(), "PAY-142")

	require.Error(t, err)
}

func TestKeysThatAreNotTicketKeysNeverReachTheFileSystem(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "PAY-142.md", pay142, now)
	tr := newTracker(t, dir, now)

	for _, key := range []string{"../PAY-142", "pay-142", "PAY-142/../x", ""} {
		_, err := tr.Ticket(context.Background(), id.ChangeID(key))
		require.Error(t, err, key)
	}
}

func TestChangesListsOnlyFilesTouchedInsideTheWindow(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "PAY-100.md", "# PAY-100: old\n", now.Add(-2*time.Hour))
	write(t, dir, "PAY-142.md", pay142, now.Add(-5*time.Minute))
	tr := newTracker(t, dir, now)

	changes, cursor, err := tr.Changes(context.Background(), ports.Cursor(poll.Format(now.Add(-time.Hour))))

	require.NoError(t, err)
	require.Len(t, changes, 1)
	require.Equal(t, "PAY-142", string(changes[0].Key))
	require.Equal(t, digest(pay142), changes[0].ChangelogID, "the changelog id is the digest of the file content")
	require.Equal(t, []string{"file"}, changes[0].Fields)
	require.NoError(t, changes[0].Validate())
	require.Equal(t, ports.Cursor(poll.Format(now)), cursor)
}

func TestAFileModifiedInsideTheOverlapIsSeen(t *testing.T) {
	dir := t.TempDir()
	// The cursor is 09:00, so the window starts at 08:50. The file was written at 08:55: before the cursor, inside
	// the overlap.
	write(t, dir, "PAY-142.md", pay142, time.Date(2026, 11, 4, 8, 55, 0, 0, time.UTC))
	tr := newTracker(t, dir, now)
	changes, _, err := tr.Changes(context.Background(), poll.Format(time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC)))

	require.NoError(t, err)
	require.Len(t, changes, 1, "a file written 5 minutes before the cursor is inside the 10-minute overlap")
}

func TestAnUnchangedFileInTheOverlapIsNotReportedAgain(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "PAY-142.md", pay142, now.Add(-time.Minute))
	clk := &fixedClock{at: now}
	tr, err := New(dir, clk)
	require.NoError(t, err)

	first, cursor, err := tr.Changes(context.Background(), "")
	require.NoError(t, err)
	require.Len(t, first, 1)

	clk.at = now.Add(time.Minute)
	second, _, err := tr.Changes(context.Background(), cursor)
	require.NoError(t, err)
	require.Empty(t, second, "the same file inside the overlap window is a repeat (dedupe by key and changelog id)")
}

func TestAnEditedFileIsReportedAgain(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "PAY-142.md", pay142, now.Add(-time.Minute))
	clk := &fixedClock{at: now}
	tr, err := New(dir, clk)
	require.NoError(t, err)
	_, cursor, err := tr.Changes(context.Background(), "")
	require.NoError(t, err)

	edited := pay142 + "3. A new criterion.\n"
	write(t, dir, "PAY-142.md", edited, now.Add(time.Minute))
	clk.at = now.Add(2 * time.Minute)
	changes, _, err := tr.Changes(context.Background(), cursor)

	require.NoError(t, err)
	require.Len(t, changes, 1)
	require.Equal(t, digest(edited), changes[0].ChangelogID)
}

func TestOnlyMarkdownTicketFilesAreRead(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "notes.txt", "PAY-7 is not a ticket file\n", now)
	write(t, dir, "PAY-142.md", pay142, now)

	changes, _, err := newTracker(t, dir, now).Changes(context.Background(), "")

	require.NoError(t, err)
	require.Len(t, changes, 1)
	require.Equal(t, "PAY-142", string(changes[0].Key))
}
