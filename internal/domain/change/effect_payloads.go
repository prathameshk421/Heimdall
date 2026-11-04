package change

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

// DefaultBranchTemplate is the branch name of a change when the workflow sets no name (TECH_SPEC 5.1).
const DefaultBranchTemplate = "{ticket}-{slug}"

// Bounds on the effect payloads. They keep an outbox row small and refuse a workflow that asks for something absurd.
const (
	maxInvite           = 32
	maxInviteLen        = 160
	maxTemplateLen      = 128
	placeholderTicket   = "{ticket}"
	placeholderSlug     = "{slug}"
	assigneePlaceholder = "person:$assignee"
)

var (
	reInviteRole   = regexp.MustCompile(`^role:[a-z][a-z0-9-]{0,62}$`)
	reTemplateSafe = regexp.MustCompile(`^[A-Za-z0-9._/-]*$`)
)

// RoomOpenPayload is the payload of a room_open effect. The title is not here: the executor reads it from the tracker.
// Invite entries are "role:<name>" or "person:<id>".
type RoomOpenPayload struct {
	Ticket id.ChangeID `json:"ticket"`
	Invite []string    `json:"invite"`
}

// Validate returns every problem with the payload.
func (p RoomOpenPayload) Validate() error {
	var v problems
	v.add("ticket", checkTicket(p.Ticket))
	v.require("invite", len(p.Invite) <= maxInvite, fmt.Sprintf("has more than %d entries", maxInvite))
	v.each("invite", p.Invite, checkInviteEntry)
	return v.join()
}

// BranchPayload is the payload of a github_branch effect. Template holds the placeholders {ticket} and {slug}. It
// starts with {ticket}-, so every branch name starts with the ticket key.
type BranchPayload struct {
	Repo     Repo        `json:"repo"`
	Ticket   id.ChangeID `json:"ticket"`
	Template string      `json:"template"`
}

// Validate returns every problem with the payload.
func (p BranchPayload) Validate() error {
	var v problems
	v.add("repo", checkRepo(p.Repo))
	v.add("ticket", checkTicket(p.Ticket))
	v.add("template", checkBranchTemplate(p.Template))
	return v.join()
}

// Name expands the template with the ticket key and the slug of the ticket title.
func (p BranchPayload) Name(slug string) string {
	name := strings.ReplaceAll(p.Template, placeholderTicket, string(p.Ticket))
	return strings.ReplaceAll(name, placeholderSlug, slug)
}

// DecodeRoomOpenPayload strictly decodes and validates a room_open payload: one JSON object, both fields present, no
// unknown field, nothing after the object.
func DecodeRoomOpenPayload(raw json.RawMessage) (RoomOpenPayload, error) {
	var p RoomOpenPayload
	if err := decodeEffectPayload(raw, []string{"ticket", "invite"}, &p); err != nil {
		return RoomOpenPayload{}, fmt.Errorf("room_open payload: %w", err)
	}
	return p, nil
}

// DecodeBranchPayload strictly decodes and validates a github_branch payload.
func DecodeBranchPayload(raw json.RawMessage) (BranchPayload, error) {
	var p BranchPayload
	if err := decodeEffectPayload(raw, []string{"repo", "ticket", "template"}, &p); err != nil {
		return BranchPayload{}, fmt.Errorf("github_branch payload: %w", err)
	}
	return p, nil
}

// InviteWithAssignee replaces the $assignee placeholder in an invite list with person:<assignee>. The list is copied,
// so the caller's slice does not change. An entry that is not the placeholder is kept as it is.
func InviteWithAssignee(invite []string, assignee ActorID) []string {
	out := make([]string, 0, len(invite))
	for _, entry := range invite {
		if entry == assigneePlaceholder {
			entry = "person:" + string(assignee)
		}
		out = append(out, entry)
	}
	return out
}

// CheckInviteTemplate validates a room invite list as written in a workflow: entries may hold the $assignee
// placeholder. It is the check that a with object of switch.open_room passes.
func CheckInviteTemplate(invite []string) error {
	if invite == nil {
		return errors.New("invite must be a list, not null")
	}
	var v problems
	v.require("invite", len(invite) <= maxInvite, fmt.Sprintf("has more than %d entries", maxInvite))
	for i, entry := range invite {
		if entry == assigneePlaceholder {
			continue
		}
		v.add(fmt.Sprintf("invite[%d]", i), checkInviteEntry(entry))
	}
	return v.join()
}

// CheckBranchTemplate validates a branch name template as written in a workflow.
func CheckBranchTemplate(template string) error {
	return checkBranchTemplate(template)
}

// decodeEffectPayload refuses a payload that is not one JSON object, lacks a required key, holds a key the type does
// not know, or has data after the object. Then it validates the value.
func decodeEffectPayload(raw json.RawMessage, required []string, v interface{ Validate() error }) error {
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keys); err != nil {
		return fmt.Errorf("not a JSON object: %w", err)
	}
	if keys == nil {
		return errors.New("must be a JSON object, not null")
	}
	for _, k := range required {
		if _, ok := keys[k]; !ok {
			return fmt.Errorf("missing field %q", k)
		}
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("decode: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return errors.New("has data after the object")
	}
	return v.Validate()
}

// checkInviteEntry accepts "role:<name>" and "person:<id>". The placeholder is refused here: CheckInviteTemplate
// accepts it, and a payload never holds it.
func checkInviteEntry(entry string) error {
	if len(entry) > maxInviteLen {
		return fmt.Errorf("is longer than %d bytes", maxInviteLen)
	}
	if reInviteRole.MatchString(entry) {
		return nil
	}
	person, ok := strings.CutPrefix(entry, "person:")
	if !ok {
		return fmt.Errorf("%q is neither role:<name> nor person:<id>", entry)
	}
	if strings.HasPrefix(person, "$") {
		return fmt.Errorf("person:%s is a placeholder, and placeholders must be replaced before the payload is built", person)
	}
	_, err := ParseActorID(person)
	return err
}

// checkBranchTemplate requires the ticket placeholder, then a hyphen, then only safe characters and the two
// placeholders. The VCS adapter applies git's full rules to the expanded name.
func checkBranchTemplate(t string) error {
	if t == "" || len(t) > maxTemplateLen {
		return fmt.Errorf("must be 1 to %d bytes", maxTemplateLen)
	}
	if !strings.HasPrefix(t, placeholderTicket+"-") {
		return fmt.Errorf("%q must start with {ticket}-", t)
	}
	rest := strings.ReplaceAll(strings.ReplaceAll(t, placeholderTicket, ""), placeholderSlug, "")
	if !reTemplateSafe.MatchString(rest) || strings.Contains(rest, "..") {
		return fmt.Errorf("%q holds a character that is not allowed in a branch name", t)
	}
	return nil
}
