package change

import (
	"fmt"
	"strings"
)

func mergeRows() []Row {
	return []Row{rowTrackerMerge(), rowClose()}
}

// rowTrackerCommit comments on the ticket after the verified tests are committed to its branch (F1).
func rowTrackerCommit() Row {
	return Row{ID: "tracker_commit", Flow: FlowTests, After: []string{"commit"}, Jobs: []EffectKind{EffectTrackerWrite},
		Done: func(in In) bool { return in.View.TrackerUpdated[TrackerStageCommit] },
		Next: func(in In) []Action {
			v := in.View
			p := TrackerWritePayload{
				Ticket:  v.Trigger.Ticket,
				Stage:   TrackerStageCommit,
				Comment: commitTrackerComment(v),
			}
			// Empty Text: commit write-back is bookkeeping and does not block the pull request or change existing Missing goldens.
			return []Action{{
				Job: &Job{Kind: EffectTrackerWrite, Key: in.jobKey("tracker_commit", string(v.Tests.Committed.SHA)), Target: string(v.Trigger.Ticket), Payload: p}}}
		}}
}

// rowTrackerMerge updates the ticket after its pull request merges: a comment with the gate summary and, when the
// policy names tracker.merge_status, a status transition (F1).
func rowTrackerMerge() Row {
	return Row{ID: "tracker_merge", Flow: FlowMerge, Jobs: []EffectKind{EffectTrackerWrite},
		Done: func(in In) bool { return in.View.Trigger == nil || in.View.TrackerUpdated[TrackerStageMerge] },
		Next: func(in In) []Action {
			v := in.View
			p := TrackerWritePayload{
				Ticket:     v.Trigger.Ticket,
				Stage:      TrackerStageMerge,
				Comment:    mergeTrackerComment(v),
				Transition: strings.TrimSpace(in.Policy.Tracker.MergeStatus),
			}
			return []Action{{
				Job: &Job{Kind: EffectTrackerWrite, Key: in.jobKey("tracker_merge", string(v.Closed.HeadSHA)), Target: string(v.Trigger.Ticket), Payload: p}}}
		}}
}

// rowClose posts a final merge summary to the change's room and archives it after merge (F2).
func rowClose() Row {
	return Row{ID: "close", Flow: FlowMerge, Jobs: []EffectKind{EffectRoomPost, EffectRoomClose},
		Done: func(in In) bool { return in.View.Room == "" || in.View.RoomClosed },
		Next: func(in In) []Action {
			v := in.View
			summary := RoomPostPayload{RoomID: v.Room, Text: closeRoomSummary(in.Change, v)}
			closeJob := RoomClosePayload{Change: in.Change, RoomID: v.Room}
			return []Action{
				{Job: &Job{Kind: EffectRoomPost, Key: in.jobKey("close", "summary"), Target: string(v.Room), Payload: summary}},
				{Job: &Job{Kind: EffectRoomClose, Key: in.jobKey("close", "archive"), Target: string(v.Room), Payload: closeJob}},
			}
		}}
}

func commitTrackerComment(v View) string {
	branch := branchName(v)
	sha := ""
	if v.Tests != nil && v.Tests.Committed != nil {
		sha = short(string(v.Tests.Committed.SHA))
	}
	return fmt.Sprintf("Heimdall verified and committed acceptance tests to branch %s (commit %s).", branch, sha)
}

func mergeTrackerComment(v View) string {
	var parts []string
	if v.PR != nil {
		parts = append(parts, fmt.Sprintf("Pull request %s#%d merged at %s.", v.PR.Repo, v.PR.Number, short(string(v.Closed.HeadSHA))))
	} else {
		parts = append(parts, fmt.Sprintf("Change merged at %s.", short(string(v.Closed.HeadSHA))))
	}
	if v.PR != nil && v.PR.Gate != nil {
		if v.PR.Gate.OK {
			parts = append(parts, "Heimdall gate: passed.")
		} else {
			parts = append(parts, "Heimdall gate: did not pass.")
		}
	}
	if len(v.Approvals) > 0 {
		last := v.Approvals[len(v.Approvals)-1]
		parts = append(parts, fmt.Sprintf("Approved by %s.", last.Login))
	}
	return strings.Join(parts, " ")
}

func closeRoomSummary(cid any, v View) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Change %v merged at %s.", cid, short(string(v.Closed.HeadSHA)))
	if v.PR != nil {
		fmt.Fprintf(&b, " PR %s#%d.", v.PR.Repo, v.PR.Number)
	}
	if len(v.Reviews) > 0 {
		fmt.Fprintf(&b, " Reviews: %d, findings: %d.", len(v.Reviews), len(v.Findings))
	}
	if len(v.Approvals) > 0 {
		last := v.Approvals[len(v.Approvals)-1]
		fmt.Fprintf(&b, " Approved by %s.", last.Login)
	}
	b.WriteString(" Closing this room.")
	return b.String()
}
