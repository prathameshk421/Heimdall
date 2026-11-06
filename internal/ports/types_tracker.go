package ports

import (
	"regexp"
	"strconv"
	"time"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

var (
	reCriterionID = regexp.MustCompile(`^AC[1-9][0-9]{0,2}$`)
	reHexDigest   = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// Cursor is an opaque position in a stream (a Jira poll, a Switch read). Its meaning belongs to the adapter that
// issued it; the application stores it and passes it back.
type Cursor string

// Criterion is one acceptance criterion of a ticket. Hash is the SHA-256 of its text, so an edit that keeps the
// text keeps the id (TECH_SPEC D50). Text is untrusted and is read only through a tool.
type Criterion struct {
	ID   string `json:"id"`
	Hash string `json:"hash"`
	Text string `json:"text"`
}

// Ticket is a ticket as the tracker reports it. Key is a ticket key (PAY-142 or KAN-7).
type Ticket struct {
	Key      id.ChangeID    `json:"key"`
	Title    string         `json:"title"`
	Assignee change.ActorID `json:"assignee"`
	Status   string         `json:"status"`
	Criteria []Criterion    `json:"criteria"`
}

// Validate returns every problem with the ticket, naming every bad criterion.
func (t Ticket) Validate() error {
	var v problems
	v.add("key", checkTicket(t.Key))
	v.add("title", checkText(t.Title, 512))
	v.add("assignee", checkActor(t.Assignee))
	v.add("status", checkText(t.Status, 64))
	for i, c := range t.Criteria {
		field := "criteria[" + strconv.Itoa(i) + "]"
		v.require(field+".id", reCriterionID.MatchString(c.ID), "must look like AC2")
		v.require(field+".hash", reHexDigest.MatchString(c.Hash), "must be 64 lowercase hex characters")
		v.add(field+".text", checkText(c.Text, MaxUntrustedText))
	}
	return v.join()
}

// TicketChange is one change to a ticket seen by a poll. ChangelogID with Key dedupes repeated polls (TECH_SPEC 13).
type TicketChange struct {
	Key         id.ChangeID `json:"key"`
	ChangelogID string      `json:"changelog_id"`
	Fields      []string    `json:"fields"`
	At          time.Time   `json:"at"`
}

// Validate returns every problem with the change.
func (c TicketChange) Validate() error {
	var v problems
	v.add("key", checkTicket(c.Key))
	v.add("changelog_id", checkText(c.ChangelogID, 128))
	v.require("fields", len(c.Fields) > 0, "must name at least one field")
	v.each("fields", c.Fields, func(f string) error { return checkText(f, 128) })
	return v.join()
}

// TicketDraft is a ticket a policy-allowed write creates, such as a rejection ticket (TECH_SPEC 13).
type TicketDraft struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

// Validate returns every problem with the draft.
func (d TicketDraft) Validate() error {
	var v problems
	v.add("title", checkText(d.Title, 512))
	v.require("body", len(d.Body) <= MaxUntrustedText, "is too long")
	return v.join()
}

// RoomRequest asks for the room of a change. Key makes the request idempotent: a repeat adopts the room it made.
type RoomRequest struct {
	ChangeID id.ChangeID      `json:"change_id"`
	Title    string           `json:"title"`
	Key      change.EffectKey `json:"key"`
}

// Validate returns every problem with the request.
func (r RoomRequest) Validate() error {
	var v problems
	v.add("change_id", checkTicketOrPR(r.ChangeID))
	v.add("title", checkText(r.Title, 256))
	v.add("key", checkText(string(r.Key), 256))
	return v.join()
}

// checkTicketOrPR accepts any change id: a ticket key or a pr- stream.
func checkTicketOrPR(c id.ChangeID) error {
	_, err := id.ParseChangeID(string(c))
	return err
}

// Message is one message read from a room. Text is untrusted (a person or an agent wrote it).
type Message struct {
	ID     change.MessageID `json:"id"`
	Room   change.RoomID    `json:"room"`
	Sender change.SenderID  `json:"sender"`
	Text   string           `json:"text"`
	At     time.Time        `json:"at"`
}

// Validate returns every problem with the message.
func (m Message) Validate() error {
	var v problems
	_, err := change.ParseMessageID(string(m.ID))
	v.add("id", err)
	_, err = change.ParseRoomID(string(m.Room))
	v.add("room", err)
	_, err = change.ParseSenderID(string(m.Sender))
	v.add("sender", err)
	v.add("text", checkText(m.Text, MaxUntrustedText))
	v.require("at", !m.At.IsZero(), "is required")
	return v.join()
}

// BlobClass says how long a blob is kept (TECH_SPEC 4.3).
type BlobClass string

// The blob classes. Logs and diffs are kept 90 days; packs are deleted on revocation; evidence is kept while a
// decision, verdict or approval refers to it.
const (
	BlobLog       BlobClass = "log"
	BlobDiff      BlobClass = "diff"
	BlobBundle    BlobClass = "bundle"
	BlobPack      BlobClass = "pack"
	BlobEvidence  BlobClass = "evidence"
	BlobKnowledge BlobClass = "knowledge"
)

// Valid reports whether c is a blob class.
func (c BlobClass) Valid() bool {
	switch c {
	case BlobLog, BlobDiff, BlobBundle, BlobPack, BlobEvidence, BlobKnowledge:
		return true
	}
	return false
}
