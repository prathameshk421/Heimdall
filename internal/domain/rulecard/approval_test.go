package rulecard

import (
	"testing"
	"time"
)

func candidateCard() Card {
	return Card{Body: "rule", AppliesTo: []string{"internal/pay/**"}, Status: StatusCandidate}
}

func TestApproveCandidate(t *testing.T) {
	at := time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC)
	got, err := Approve(candidateCard(), "alice", at)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusApproved || got.ApprovedBy != "alice" || !got.ApprovedAt.Equal(at) || !CanShare(got) {
		t.Fatalf("approved card = %#v", got)
	}
}

func TestApprovalTransitionsRefuseInvalidStateOrInput(t *testing.T) {
	at := time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC)
	approved, err := Approve(candidateCard(), "alice", at)
	if err != nil {
		t.Fatal(err)
	}
	for name, card := range map[string]Card{
		"double approve": approved,
		"rejected":       {Status: StatusRejected},
	} {
		if _, err := Approve(card, "alice", at); err == nil {
			t.Errorf("Approve %s succeeded", name)
		}
	}
	if _, err := Approve(candidateCard(), " ", at); err == nil {
		t.Error("empty approver accepted")
	}
	if _, err := Approve(candidateCard(), "alice", time.Time{}); err == nil {
		t.Error("zero approval time accepted")
	}
}

func TestEditAndApproveRevalidatesCandidate(t *testing.T) {
	at := time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC)
	got, err := EditAndApprove(candidateCard(), "edited", []string{"internal/pay/refunds/**"}, "alice", at)
	if err != nil {
		t.Fatal(err)
	}
	if got.Body != "edited" || len(got.AppliesTo) != 1 || got.AppliesTo[0] != "internal/pay/refunds/**" || got.Status != StatusApproved {
		t.Fatalf("edited approved card = %#v", got)
	}
	if _, err := EditAndApprove(candidateCard(), "edited", []string{"../outside/**"}, "alice", at); err == nil {
		t.Error("invalid edit accepted")
	}
	if _, err := EditAndApprove(candidateCard(), " ", []string{"internal/pay/**"}, "alice", at); err == nil {
		t.Error("empty card text accepted")
	}
	if _, err := EditAndApprove(Card{Status: StatusApproved}, "edited", nil, "alice", at); err == nil {
		t.Error("edit of approved card accepted")
	}
}

func TestRejectCandidateAndRefuseInvalidTransition(t *testing.T) {
	at := time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC)
	got, err := Reject(candidateCard(), "alice", "not useful", at)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusRejected || got.RejectedBy != "alice" || got.RejectionReason != "not useful" || !got.RejectedAt.Equal(at) || CanShare(got) {
		t.Fatalf("rejected card = %#v", got)
	}
	for name, card := range map[string]Card{"approved": {Status: StatusApproved}, "rejected": got} {
		if _, err := Reject(card, "alice", "reason", at); err == nil {
			t.Errorf("Reject %s succeeded", name)
		}
	}
	if _, err := Reject(candidateCard(), "alice", " ", at); err == nil {
		t.Error("empty rejection reason accepted")
	}
	if _, err := Reject(candidateCard(), " ", "reason", at); err == nil {
		t.Error("empty approver accepted")
	}
	if _, err := Reject(candidateCard(), "alice", "reason", time.Time{}); err == nil {
		t.Error("zero rejection time accepted")
	}
}

func TestCanShareRequiresApprovedAndValid(t *testing.T) {
	at := time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC)
	approved, err := Approve(candidateCard(), "alice", at)
	if err != nil {
		t.Fatal(err)
	}
	if !CanShare(approved) {
		t.Error("valid approved card cannot be shared")
	}
	if CanShare(candidateCard()) {
		t.Error("candidate card can be shared")
	}
	if CanShare(Card{Status: StatusRejected}) {
		t.Error("rejected card can be shared")
	}
	approved.Triggers = []string{"x"}
	if CanShare(approved) {
		t.Error("invalid approved card can be shared")
	}
}
