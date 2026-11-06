package commands

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Usage lines, without the prefix. They are shown in every bad-arguments reply
// and in the unknown-command grammar, so they are the one place the grammar lives.
const (
	useUphold           = "uphold <finding>"
	useDismiss          = "dismiss <finding> <reason of at least 15 characters>"
	useStart            = "start <ticket>"
	useReview           = "review @a @b @c"
	useApprove          = "approve-tests"
	useApproveProfile   = "approve-profile <proposal_id>"
	useApproveKnowledge = "approve-knowledge <knowledge_id>"
	usePromoteProfile   = "promote-profile <proposal_id>"
	useChallenge        = "challenge <finding> <evidence>"
	useRemember         = "remember <text>"
	useForget           = "forget <memory_ref> <reason>"
	useSpec             = "spec-change <text>"
	useReject           = "reject <reason>"
	useOverride         = "override <case> <reason>"
	useWaive            = "waive-reviewer @x <reason>"
	useIncident         = "incident <text>"
	useResume           = "resume <step>"
)

// parseUphold takes exactly one finding.
func parseUphold(p, args string) (Uphold, error) {
	f, rest := cutWord(args)
	finding, err := parseFinding(f)
	if err != nil {
		return Uphold{}, fail(p, useUphold, err)
	}
	if rest != "" {
		return Uphold{}, fail(p, useUphold, errors.New("uphold takes one finding"))
	}
	return Uphold{Finding: finding}, nil
}

// parseDismiss takes a finding and a reason of at least 15 characters. The
// reason counts runes, not bytes, so accented text is not counted twice.
func parseDismiss(p, args string) (Dismiss, error) {
	f, reason := cutWord(args)
	finding, err := parseFinding(f)
	if err != nil {
		return Dismiss{}, fail(p, useDismiss, err)
	}
	if utf8.RuneCountInString(reason) < minDismissReason {
		return Dismiss{}, fail(p, useDismiss, errShortReason)
	}
	return Dismiss{Finding: finding, Reason: reason}, nil
}

// parseStart takes one ticket key.
func parseStart(p, args string) (Start, error) {
	t, rest := cutWord(args)
	ticket, err := parseTicket(t)
	if err != nil {
		return Start{}, fail(p, useStart, err)
	}
	if rest != "" {
		return Start{}, fail(p, useStart, errors.New("start takes one ticket"))
	}
	return Start{Ticket: ticket}, nil
}

// parseReview takes one to three reviewers.
func parseReview(p, args string) (Review, error) {
	reviewers, err := parseReviewers(args)
	if err != nil {
		return Review{}, fail(p, useReview, err)
	}
	return Review{Reviewers: reviewers}, nil
}

// parseResume takes one workflow step id, as the escalation names it. The step id is lower case, as the workflow writes it.
func parseResume(p, args string) (Resume, error) {
	step, rest := cutWord(args)
	if step == "" || rest != "" {
		return Resume{}, fail(p, useResume, errors.New("resume takes one step id"))
	}
	if !stepRE.MatchString(step) {
		return Resume{}, fail(p, useResume, fmt.Errorf("%q is not a workflow step id", step))
	}
	return Resume{Step: step}, nil
}

// checkApproveTests refuses any argument. The verb takes none.
func checkApproveTests(p, args string) error {
	if args != "" {
		return fail(p, useApprove, errors.New("approve-tests takes no arguments"))
	}
	return nil
}

func parseApproveProfile(p, args string) (ApproveProfile, error) {
	id, err := parseProfileProposalID(args)
	if err != nil {
		return ApproveProfile{}, fail(p, useApproveProfile, err)
	}
	return ApproveProfile{ID: id}, nil
}

func parsePromoteProfile(p, args string) (PromoteProfile, error) {
	id, err := parseProfileProposalID(args)
	if err != nil {
		return PromoteProfile{}, fail(p, usePromoteProfile, err)
	}
	return PromoteProfile{ID: id}, nil
}

// parseChallenge takes a finding and the evidence, which may run to several
// words. The evidence is kept as typed.
func parseChallenge(p, args string) (Challenge, error) {
	f, evidence := cutWord(args)
	finding, err := parseFinding(f)
	if err != nil {
		return Challenge{}, fail(p, useChallenge, err)
	}
	if evidence == "" {
		return Challenge{}, fail(p, useChallenge, errNoEvidence)
	}
	return Challenge{Finding: finding, Evidence: evidence}, nil
}

// parseRemember takes free text. Its line breaks are kept.
func parseRemember(p, args string) (Remember, error) {
	text, err := freeText(p, useRemember, "remember", args)
	if err != nil {
		return Remember{}, err
	}
	return Remember{Text: text}, nil
}

// parseSpecChange takes free text. Its line breaks are kept.
func parseSpecChange(p, args string) (SpecChange, error) {
	text, err := freeText(p, useSpec, "spec-change", args)
	if err != nil {
		return SpecChange{}, err
	}
	return SpecChange{Text: text}, nil
}

// parseIncident takes free text. Its line breaks are kept.
func parseIncident(p, args string) (Incident, error) {
	text, err := freeText(p, useIncident, "incident", args)
	if err != nil {
		return Incident{}, err
	}
	return Incident{Text: text}, nil
}

// parseReject takes a reason as free text.
func parseReject(p, args string) (Reject, error) {
	reason, err := freeText(p, useReject, "reject", args)
	if err != nil {
		return Reject{}, err
	}
	return Reject{Reason: reason}, nil
}

// parseForget takes a memory ref and a reason.
func parseForget(p, args string) (Forget, error) {
	ref, reason := cutWord(args)
	name, err := parseName(ref, "memory_ref")
	if strings.HasPrefix(ref, "knowledge:") && profileProposalRE.MatchString(strings.TrimPrefix(ref, "knowledge:")) {
		name, err = ref, nil
	}
	if err != nil {
		return Forget{}, fail(p, useForget, err)
	}
	if reason == "" {
		return Forget{}, fail(p, useForget, errors.New("forget needs a reason"))
	}
	return Forget{MemoryRef: name, Reason: reason}, nil
}

func parseApproveKnowledge(p, args string) (ApproveKnowledge, error) {
	value, rest := cutWord(args)
	if rest != "" || !profileProposalRE.MatchString(value) {
		return ApproveKnowledge{}, fail(p, useApproveKnowledge, errors.New("knowledge id must be 64 lowercase hex characters"))
	}
	return ApproveKnowledge{Ref: "knowledge:" + value}, nil
}

// parseOverride takes a case id and a reason.
func parseOverride(p, args string) (Override, error) {
	c, reason := cutWord(args)
	name, err := parseName(c, "case")
	if err != nil {
		return Override{}, fail(p, useOverride, err)
	}
	if reason == "" {
		return Override{}, fail(p, useOverride, errors.New("override needs a reason"))
	}
	return Override{Case: name, Reason: reason}, nil
}

// parseWaiveReviewer takes one reviewer handle and a reason.
func parseWaiveReviewer(p, args string) (WaiveReviewer, error) {
	tok, reason := cutWord(args)
	reviewer, err := parseHandle(tok)
	if err != nil {
		return WaiveReviewer{}, fail(p, useWaive, err)
	}
	if reason == "" {
		return WaiveReviewer{}, fail(p, useWaive, errors.New("waive-reviewer needs a reason"))
	}
	return WaiveReviewer{Reviewer: reviewer, Reason: reason}, nil
}

// freeText returns the trimmed text, or the usage error when it is empty.
// The verb is named in the message so the reply says which command was short.
func freeText(p, use, verb, args string) (string, error) {
	if args == "" {
		return "", fail(p, use, fmt.Errorf("%s needs text", verb))
	}
	return args, nil
}
