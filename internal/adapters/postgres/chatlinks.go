package postgres

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

var _ ports.ChatLinks = (*Store)(nil)
var _ ports.ChatLinkRemover = (*Store)(nil)

// linkCodeAlphabet has no 0/O or 1/I, so a code read off a phone screen is typed right.
const linkCodeAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// linkCodeLength gives 32^6, about a billion codes: guessing an open one inside its expiry is not practical, and it is
// short enough to type.
const linkCodeLength = 6

// OpenLinkCode returns the sender's open code, or issues a new one.
func (s *Store) OpenLinkCode(ctx context.Context, sender change.SenderID, room change.RoomID, now, expires time.Time) (ports.LinkCode, error) {
	if !expires.After(now) {
		return ports.LinkCode{}, errors.New("link code must expire after it is issued")
	}
	open := ports.LinkCode{Sender: sender}
	var roomText string
	err := s.pool.QueryRow(ctx, `SELECT code, room, expires_at FROM chat_link_code
		WHERE sender = $1 AND redeemed_at IS NULL AND expires_at > $2 ORDER BY expires_at DESC LIMIT 1`,
		string(sender), now).Scan(&open.Code, &roomText, &open.Expires)
	if err == nil {
		open.Room, open.Expires = change.RoomID(roomText), open.Expires.UTC()
		return open, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return ports.LinkCode{}, fmt.Errorf("read open link code: %w", err)
	}
	code, err := newLinkCode()
	if err != nil {
		return ports.LinkCode{}, err
	}
	// A clash with an existing code is a unique violation, returned as an error: at 32^6 codes it does not happen in
	// practice, and the sender's next message asks again.
	if _, err := s.pool.Exec(ctx, `INSERT INTO chat_link_code (code, sender, room, created_at, expires_at) VALUES ($1, $2, $3, $4, $5)`,
		code, string(sender), string(room), now, expires); err != nil {
		return ports.LinkCode{}, fmt.Errorf("issue link code: %w", err)
	}
	return ports.LinkCode{Code: code, Sender: sender, Room: room, Expires: expires.UTC()}, nil
}

// RedeemLinkCode uses an open code once and links its sender to login, in one transaction.
func (s *Store) RedeemLinkCode(ctx context.Context, code, login string, now time.Time) (ports.LinkCode, error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	if login == "" {
		return ports.LinkCode{}, errors.New("link code redeemed with no login")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return ports.LinkCode{}, fmt.Errorf("begin link code redeem: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	used := ports.LinkCode{Code: code}
	var sender, room string
	err = tx.QueryRow(ctx, `UPDATE chat_link_code SET redeemed_at = $2, redeemed_by = $3
		WHERE code = $1 AND redeemed_at IS NULL AND expires_at > $2 RETURNING sender, room, expires_at`,
		code, now, login).Scan(&sender, &room, &used.Expires)
	if errors.Is(err, pgx.ErrNoRows) {
		return ports.LinkCode{}, ports.ErrNoLinkCode
	}
	if err != nil {
		return ports.LinkCode{}, fmt.Errorf("redeem link code: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO chat_link (sender, github, linked_at, code) VALUES ($1, $2, $3, $4)
		ON CONFLICT (sender) DO UPDATE SET github = EXCLUDED.github, linked_at = EXCLUDED.linked_at, code = EXCLUDED.code`,
		sender, login, now, code); err != nil {
		return ports.LinkCode{}, fmt.Errorf("record chat link: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return ports.LinkCode{}, fmt.Errorf("commit link code redeem: %w", err)
	}
	used.Sender, used.Room, used.Expires = change.SenderID(sender), change.RoomID(room), used.Expires.UTC()
	return used, nil
}

// ChatLinks returns every link, oldest first.
func (s *Store) ChatLinks(ctx context.Context) ([]ports.ChatLink, error) {
	rows, err := s.pool.Query(ctx, `SELECT sender, github, linked_at FROM chat_link ORDER BY linked_at, sender`)
	if err != nil {
		return nil, fmt.Errorf("read chat links: %w", err)
	}
	defer rows.Close()
	var out []ports.ChatLink
	for rows.Next() {
		var l ports.ChatLink
		var sender string
		if err := rows.Scan(&sender, &l.GitHub, &l.At); err != nil {
			return nil, fmt.Errorf("scan chat link: %w", err)
		}
		l.Sender, l.At = change.SenderID(sender), l.At.UTC()
		out = append(out, l)
	}
	return out, rows.Err()
}

// DeleteChatLinks removes proven chat links matching a Switch sender id or a GitHub login (case-insensitive).
func (s *Store) DeleteChatLinks(ctx context.Context, target string) (int, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return 0, errors.New("delete chat links: target is empty")
	}
	tag, err := s.pool.Exec(ctx, `DELETE FROM chat_link WHERE sender = $1 OR lower(github) = lower($1)`, target)
	if err != nil {
		return 0, fmt.Errorf("delete chat links for %s: %w", target, err)
	}
	return int(tag.RowsAffected()), nil
}

// newLinkCode draws a code from crypto/rand. Rejection sampling keeps every letter equally likely.
func newLinkCode() (string, error) {
	var b strings.Builder
	buf := make([]byte, 1)
	for b.Len() < linkCodeLength {
		if _, err := rand.Read(buf); err != nil {
			return "", fmt.Errorf("draw link code: %w", err)
		}
		if int(buf[0]) >= 256-256%len(linkCodeAlphabet) {
			continue
		}
		b.WriteByte(linkCodeAlphabet[int(buf[0])%len(linkCodeAlphabet)])
	}
	return b.String(), nil
}
