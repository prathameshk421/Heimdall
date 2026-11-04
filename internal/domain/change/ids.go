package change

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	reEventID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	reRepo    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*/[A-Za-z0-9][A-Za-z0-9._-]*$`)
)

// maxOpaqueLen bounds ids that other systems assign (Switch, the actor's login). They are opaque to us, so the
// only checks are that they are present, printable and not absurdly long.
const maxOpaqueLen = 128

// EventID is the id of one event: a lowercase UUID, as the event table stores it (TECH_SPEC 4.1).
type EventID string

// ParseEventID validates s.
func ParseEventID(s string) (EventID, error) {
	if !reEventID.MatchString(s) {
		return "", fmt.Errorf("event id %q is not a lowercase UUID", s)
	}
	return EventID(s), nil
}

// String returns the id as text.
func (e EventID) String() string { return string(e) }

// UnmarshalText validates while decoding.
func (e *EventID) UnmarshalText(b []byte) error {
	v, err := ParseEventID(string(b))
	*e = v
	return err
}

// Repo is a GitHub repository as owner/name.
type Repo string

// ParseRepo validates s.
func ParseRepo(s string) (Repo, error) {
	if !reRepo.MatchString(s) {
		return "", fmt.Errorf("repository %q is not owner/name", s)
	}
	return Repo(s), nil
}

// String returns the repository as text.
func (r Repo) String() string { return string(r) }

// UnmarshalText validates while decoding.
func (r *Repo) UnmarshalText(b []byte) error {
	v, err := ParseRepo(string(b))
	*r = v
	return err
}

// RoomID is the Switch room a change talks in (one room per change, TECH_SPEC 13).
type RoomID string

// SenderID is the Switch sender id of a chat participant. It is matched whole against people (D140).
type SenderID string

// MessageID is the Switch id of one chat message.
type MessageID string

// QuestionID names one question a test-author or reviewer asked in a room.
type QuestionID string

// CorrelationID is the trace id that ties events caused by one request together.
type CorrelationID string

// ActorID names the actor of an event within its kind: a login, a Switch sender id, an agent name or a system
// component.
type ActorID string

// ParseRoomID validates s.
func ParseRoomID(s string) (RoomID, error) {
	v, err := parseOpaque[RoomID]("room id", s)
	return v, err
}

// ParseSenderID validates s.
func ParseSenderID(s string) (SenderID, error) {
	v, err := parseOpaque[SenderID]("sender id", s)
	return v, err
}

// ParseMessageID validates s.
func ParseMessageID(s string) (MessageID, error) {
	v, err := parseOpaque[MessageID]("message id", s)
	return v, err
}

// ParseQuestionID validates s.
func ParseQuestionID(s string) (QuestionID, error) {
	v, err := parseOpaque[QuestionID]("question id", s)
	return v, err
}

// ParseCorrelationID validates s.
func ParseCorrelationID(s string) (CorrelationID, error) {
	v, err := parseOpaque[CorrelationID]("correlation id", s)
	return v, err
}

// ParseActorID validates s.
func ParseActorID(s string) (ActorID, error) {
	v, err := parseOpaque[ActorID]("actor id", s)
	return v, err
}

// parseOpaque checks an id we do not generate: non-empty, at most maxOpaqueLen bytes, printable ASCII and no spaces.
func parseOpaque[T ~string](kind, s string) (T, error) {
	if s == "" || len(s) > maxOpaqueLen {
		return "", fmt.Errorf("%s must be 1 to %d characters", kind, maxOpaqueLen)
	}
	for _, r := range s {
		if r <= ' ' || r > '~' {
			return "", fmt.Errorf("%s %q holds a character outside printable ASCII without spaces", kind, s)
		}
	}
	if strings.TrimSpace(s) != s {
		return "", fmt.Errorf("%s %q has surrounding space", kind, s)
	}
	return T(s), nil
}

// String returns the id as text.
func (r RoomID) String() string { return string(r) }

// String returns the id as text.
func (s SenderID) String() string { return string(s) }

// String returns the id as text.
func (m MessageID) String() string { return string(m) }

// String returns the id as text.
func (q QuestionID) String() string { return string(q) }

// String returns the id as text.
func (c CorrelationID) String() string { return string(c) }

// String returns the id as text.
func (a ActorID) String() string { return string(a) }

// UnmarshalText validates while decoding.
func (r *RoomID) UnmarshalText(b []byte) error {
	v, err := ParseRoomID(string(b))
	*r = v
	return err
}

// UnmarshalText validates while decoding.
func (s *SenderID) UnmarshalText(b []byte) error {
	v, err := ParseSenderID(string(b))
	*s = v
	return err
}

// UnmarshalText validates while decoding.
func (m *MessageID) UnmarshalText(b []byte) error {
	v, err := ParseMessageID(string(b))
	*m = v
	return err
}

// UnmarshalText validates while decoding.
func (q *QuestionID) UnmarshalText(b []byte) error {
	v, err := ParseQuestionID(string(b))
	*q = v
	return err
}

// UnmarshalText validates while decoding.
func (c *CorrelationID) UnmarshalText(b []byte) error {
	v, err := ParseCorrelationID(string(b))
	*c = v
	return err
}

// UnmarshalText validates while decoding.
func (a *ActorID) UnmarshalText(b []byte) error {
	v, err := ParseActorID(string(b))
	*a = v
	return err
}
