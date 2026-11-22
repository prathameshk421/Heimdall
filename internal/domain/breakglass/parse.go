package breakglass

import (
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
)

// Comment is the identity and metadata needed to evaluate a PR issue comment.
type Comment struct {
	ID                    int64
	Login, UserType, Body string
	CreatedAt, UpdatedAt  time.Time
}

// Kind identifies a valid break-glass command.
type Kind string

const (
	// Record opens a bypass for the current head.
	Record Kind = "record"
	// Review closes an open bypass by another authorized person.
	Review Kind = "review"
)

// DropReason explains why a candidate comment was ignored.
type DropReason string

const (
	// DropEdited means the comment's creation and update times differ.
	DropEdited DropReason = "edited"
	// DropInvalid means the comment is malformed or outside supported bounds.
	DropInvalid DropReason = "invalid"
	// DropBot means the comment was authored by a bot.
	DropBot DropReason = "bot"
	// DropUnauthorized means the login is not in the policy allowlist.
	DropUnauthorized DropReason = "unauthorized"
	// DropShortReason means the reason is shorter than policy requires.
	DropShortReason DropReason = "short_reason"
	// DropSelfReview means no other authorized person has an open bypass to review.
	DropSelfReview DropReason = "self_review"
)

// Command is a parsed break-glass command.
type Command struct {
	Kind                     Kind
	Login, Reason, SourceRef string
}

// Recorded is the open bypass used to check a review command.
type Recorded struct {
	Login string
	Head  string
}

// Parse accepts only unedited comments from allowlisted human logins. Recorded is the
// open record on this head and is used to prevent self-review.
func Parse(c Comment, allow []string, head string, recorded *Recorded) (Command, DropReason) {
	if drop := candidateDrop(c, allow); drop != "" {
		return Command{}, drop
	}
	body := strings.TrimSpace(c.Body)
	const prefix = "/heimdall "
	if !strings.HasPrefix(body, prefix) {
		return Command{}, DropInvalid
	}
	return parseBody(strings.TrimPrefix(body, prefix), c, head, recorded)
}

func candidateDrop(c Comment, allow []string) DropReason {
	if c.CreatedAt.IsZero() || c.UpdatedAt.IsZero() || !c.CreatedAt.Equal(c.UpdatedAt) {
		return DropEdited
	}
	if c.ID <= 0 || strings.TrimSpace(c.Login) == "" {
		return DropInvalid
	}
	if strings.EqualFold(c.UserType, "bot") || strings.HasSuffix(strings.ToLower(c.Login), "[bot]") {
		return DropBot
	}
	for _, login := range allow {
		if strings.EqualFold(login, c.Login) {
			return ""
		}
	}
	return DropUnauthorized
}

func parseBody(cmd string, c Comment, head string, recorded *Recorded) (Command, DropReason) {
	if strings.HasPrefix(cmd, "break-glass ") {
		return parseRecord(strings.TrimPrefix(cmd, "break-glass "), c)
	}
	if cmd == "break-glass-reviewed" {
		if recorded == nil || recorded.Head != head || strings.EqualFold(recorded.Login, c.Login) {
			return Command{}, DropSelfReview
		}
		return Command{Kind: Review, Login: c.Login, SourceRef: source(c.ID)}, ""
	}
	return Command{}, DropInvalid
}

func parseRecord(raw string, c Comment) (Command, DropReason) {
	reason := strings.TrimSpace(raw)
	n := utf8.RuneCountInString(reason)
	if n < policy.BreakGlassMinReason {
		return Command{}, DropShortReason
	}
	if n > 500 {
		return Command{}, DropInvalid
	}
	return Command{Kind: Record, Login: c.Login, Reason: reason, SourceRef: source(c.ID)}, ""
}

func source(id int64) string { return "comment-" + strconv.FormatInt(id, 10) }
