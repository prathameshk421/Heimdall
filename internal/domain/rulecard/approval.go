package rulecard

import (
	"errors"
	"strings"
	"time"
)

// Approve moves a candidate card to approved and records who approved it and when.
func Approve(card Card, approver string, at time.Time) (Card, error) {
	if card.Status != StatusCandidate {
		return Card{}, errors.New("only a candidate card can be approved")
	}
	if err := approvalInput(approver, at); err != nil {
		return Card{}, err
	}
	if err := card.Validate(); err != nil {
		return Card{}, err
	}
	card.Status = StatusApproved
	card.ApprovedBy = strings.TrimSpace(approver)
	card.ApprovedAt = at
	return card, nil
}

// EditAndApprove updates a candidate's text and path scope, validates the complete card, and approves it.
func EditAndApprove(card Card, newText string, newGlobs []string, approver string, at time.Time) (Card, error) {
	if card.Status != StatusCandidate {
		return Card{}, errors.New("only a candidate card can be edited and approved")
	}
	card.Body = newText
	card.AppliesTo = append([]string(nil), newGlobs...)
	return Approve(card, approver, at)
}

// Reject moves a candidate card to rejected and records who rejected it, why and when.
func Reject(card Card, approver, reason string, at time.Time) (Card, error) {
	if card.Status != StatusCandidate {
		return Card{}, errors.New("only a candidate card can be rejected")
	}
	if err := approvalInput(approver, at); err != nil {
		return Card{}, err
	}
	if strings.TrimSpace(reason) == "" {
		return Card{}, errors.New("rejection reason is empty")
	}
	if err := card.Validate(); err != nil {
		return Card{}, err
	}
	card.Status = StatusRejected
	card.RejectedBy = strings.TrimSpace(approver)
	card.RejectionReason = strings.TrimSpace(reason)
	card.RejectedAt = at
	return card, nil
}

// CanShare reports whether a card is approved and still passes validation.
func CanShare(card Card) bool {
	return card.Status == StatusApproved && card.Validate() == nil
}

func approvalInput(approver string, at time.Time) error {
	if strings.TrimSpace(approver) == "" {
		return errors.New("approver is empty")
	}
	if at.IsZero() {
		return errors.New("approval time is zero")
	}
	return nil
}
