package ports

import (
	"context"
	"errors"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
)

// ErrNoLinkCode says a link code is unknown, already used or expired.
var ErrNoLinkCode = errors.New("no open link code")

// LinkCode is a one-time code given in chat to a sender Heimdall does not know. Commenting it on GitHub proves which
// login the sender belongs to (D194). Room is where the sender asked, so the result is posted back there.
type LinkCode struct {
	Code    string
	Sender  change.SenderID
	Room    change.RoomID
	Expires time.Time
}

// ChatLink is a chat sender id that proved it belongs to a GitHub login.
type ChatLink struct {
	Sender change.SenderID
	GitHub string
	At     time.Time
}

// ChatLinks keeps link codes and the links they prove. Consumers: the command processor and the people directory.
type ChatLinks interface {
	// OpenLinkCode returns the sender's code that is still open at now, or issues a new one that expires at expires.
	OpenLinkCode(ctx context.Context, sender change.SenderID, room change.RoomID, now, expires time.Time) (LinkCode, error)
	// RedeemLinkCode uses an open code once and links its sender to login. An unknown, used or expired code is
	// ErrNoLinkCode. A sender linked before is linked again to the newer login.
	RedeemLinkCode(ctx context.Context, code, login string, now time.Time) (LinkCode, error)
	// ChatLinks returns every link.
	ChatLinks(ctx context.Context) ([]ChatLink, error)
}

// ChatLinkRemover deletes proven chat links by Switch sender id or GitHub login (D194 unlink).
type ChatLinkRemover interface {
	DeleteChatLinks(ctx context.Context, target string) (int, error)
}
