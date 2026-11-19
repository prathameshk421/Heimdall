package change

import (
	"fmt"
	"slices"
	"strings"
)

// defaultTestsDir is the folder of the Heimdall tests when the profile names none.
const defaultTestsDir = "tests/heimdall"

// rowOpenRoom: the room exists. It starts with the first fact of the change, whichever flow that is.
func rowOpenRoom() Row {
	return Row{ID: "open_room", Flow: FlowBoth, Jobs: []EffectKind{EffectRoomOpen},
		Done: func(in In) bool { return in.View.Room != "" },
		Next: func(in In) []Action {
			p := RoomOpenPayload{Ticket: in.Change, Invite: roomInvite(in)}
			return []Action{{Text: "the change's room is opened",
				Job: &Job{Kind: EffectRoomOpen, Key: in.jobKey("open_room", ""), Target: string(in.Change), Payload: p}}}
		}}
}

// roomInvite names who joins: the test author and the assignee when there is a ticket, and the person who owns the
// pull request's GitHub login when there is a PR.
func roomInvite(in In) []string {
	invite := []string{}
	if t := in.View.Trigger; t != nil {
		invite = append(invite, "role:test-author", "person:"+string(t.Assignee))
	}
	if pr := in.View.PR; pr != nil {
		if person, ok := personByGitHub(in, string(pr.Author)); ok && !slices.Contains(invite, "person:"+person) {
			invite = append(invite, "person:"+person)
		}
	}
	return invite
}

func personByGitHub(in In, login string) (string, bool) {
	for name, p := range in.Policy.People {
		if p.GitHub != "" && strings.EqualFold(p.GitHub, login) {
			return name, true
		}
	}
	return "", false
}

func testsRows() []Row {
	return []Row{rowBranch(), rowWriteTests(), rowTestQA(), rowSignoff(), rowCommit()}
}

func rowBranch() Row {
	return Row{ID: "branch", Flow: FlowTests, After: []string{"open_room"}, Jobs: []EffectKind{EffectGitHubBranch},
		Done: func(in In) bool { return in.View.Branch != nil },
		Next: func(in In) []Action {
			t := in.View.Trigger
			tmpl := in.Settings.BranchTemplate
			if tmpl == "" {
				tmpl = DefaultBranchTemplate
			}
			p := BranchPayload{Repo: t.Repo, Ticket: t.Ticket, Template: tmpl}
			return []Action{{Text: "the ticket branch is created",
				Job: &Job{Kind: EffectGitHubBranch, Key: in.jobKey("branch", ""), Target: string(t.Ticket), Payload: p}}}
		}}
}

// writeTestsBudget is how many submissions the test author may make: the setting, plus the same again for each resume.
func writeTestsBudget(in In) int {
	return in.Settings.TestAttempts * (1 + in.View.Resumes["write_tests"])
}

func rowWriteTests() Row {
	return Row{ID: "write_tests", Flow: FlowTests, After: []string{"branch"},
		Done: func(in In) bool { return in.View.Tests != nil && len(in.View.Tests.Rejected) == 0 },
		Next: func(in In) []Action {
			v := in.View
			attempts := 0
			if v.Tests != nil {
				attempts = v.Tests.Attempts
			}
			if attempts >= writeTestsBudget(in) && v.Tests != nil {
				text := fmt.Sprintf("tests were rejected %d times: %s; `heimdall resume %s write_tests` allows more",
					attempts, strings.Join(v.Tests.Rejected, "; "), in.Change)
				return []Action{{Ask: &Ask{Who: operators(in), What: text}, Text: text}}
			}
			return []Action{{Task: &Task{Kind: "tests", Subject: string(v.Trigger.Ticket), Input: fmt.Sprintf("attempt-%d", attempts+1)},
				Text: "the test author submits tests"}}
		}}
}

// operators are the people to tell when work is stuck: the approvers of every domain, in name order.
func operators(in In) []ActorID {
	seen := map[string]bool{}
	var out []ActorID
	for _, names := range in.Policy.Approvers {
		for _, n := range names {
			if !seen[n] {
				seen[n] = true
				out = append(out, ActorID(n))
			}
		}
	}
	slices.Sort(out)
	return out
}

func rowTestQA() Row {
	return Row{ID: "test_qa", Flow: FlowTests, After: []string{"write_tests"}, Jobs: []EffectKind{EffectTestQuality},
		Done: func(in In) bool { return in.View.Tests != nil && in.View.Tests.Verified != nil },
		Next: func(in In) []Action {
			v := in.View
			p := TestQualityPayload{Repo: v.Trigger.Repo, Branch: branchName(v), Ticket: in.Change, SubmissionRef: v.Tests.Ref,
				BaseSHA: v.Branch.BaseSHA}
			suffix := string(v.Tests.Ref)
			if v.PR != nil { // tests written while a PR exists are judged on its head too; a new head is a new job
				p.HeadSHA = v.PR.HeadSHA
				suffix += "/" + string(v.PR.HeadSHA)
			}
			return []Action{{Text: "the sandbox verifies the tests",
				Job: &Job{Kind: EffectTestQuality, Key: in.jobKey("test_qa", suffix), Target: string(in.Change), Payload: p}}}
		}}
}

func branchName(v View) string {
	if v.Branch == nil {
		return ""
	}
	return v.Branch.Branch
}

func rowSignoff() Row {
	return Row{ID: "signoff", Flow: FlowTests, After: []string{"test_qa"},
		Done: func(in In) bool {
			t := in.View.Tests
			return t != nil && t.Verified != nil && (!t.Verified.NeedsSignoff || t.ApprovedBy != "")
		},
		Next: func(in In) []Action {
			text := "a person approves the tests (sensitive domain)"
			return []Action{{Ask: &Ask{Who: operators(in), What: "heimdall approve-tests " + string(in.Change)}, Text: text}}
		}}
}

func rowCommit() Row {
	return Row{ID: "commit", Flow: FlowTests, After: []string{"test_qa", "signoff"}, Jobs: []EffectKind{EffectGitHubCommitTests},
		Done: func(in In) bool { return in.View.Tests != nil && in.View.Tests.Committed != nil },
		Next: func(in In) []Action {
			v := in.View
			dir := testsDir(in) + "/" + string(v.Trigger.Ticket)
			files := make([]CommitFile, 0, len(v.Tests.Files))
			for _, f := range v.Tests.Files {
				files = append(files, CommitFile{Path: f.Path, BlobRef: f.Blob})
			}
			if len(files) == 0 {
				files = append(files, CommitFile{Path: dir + "/test.go", BlobRef: v.Tests.Ref})
			}
			p := CommitTestsPayload{Repo: v.Trigger.Repo, Branch: branchName(v), Dir: dir, Files: files,
				Message: "tests: commit tests for " + string(in.Change), Change: in.Change}
			return []Action{{Text: "the tests are committed to the branch",
				Job: &Job{Kind: EffectGitHubCommitTests, Key: in.jobKey("commit", string(v.Tests.Ref)), Target: string(v.Trigger.Repo), Payload: p}}}
		}}
}

func testsDir(in In) string {
	if in.Profile.TestsDir != "" {
		return strings.TrimSuffix(in.Profile.TestsDir, "/")
	}
	return defaultTestsDir
}
