package ports

import (
	"context"
	"time"
)

// RevocationStore persists signed-out board session identifiers until their original expiry so a server restart
// cannot reinstate a logged-out session cookie.
type RevocationStore interface {
	// Revoke records sid as revoked until expiresAt. Repeating the same sid is idempotent.
	Revoke(ctx context.Context, sid string, expiresAt time.Time) error
	// IsRevoked reports whether sid is revoked at now.
	IsRevoked(ctx context.Context, sid string, now time.Time) (bool, error)
	// Prune deletes revocations whose expiry is at or before now.
	Prune(ctx context.Context, now time.Time) error
}
