package change

import (
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
)

// The tests area of the event catalog (TECH_SPEC 3.2): the acceptance tests from submission to commit.
const (
	EventTestsSubmitted EventType = "tests.submitted"
	EventTestsVerified  EventType = "tests.verified"
	EventTestsRejected  EventType = "tests.rejected"
	EventTestsCommitted EventType = "tests.committed"
	EventTestsApproved  EventType = "tests.approved"
)

// testsPayloads is the registry of the tests area.
func testsPayloads() map[EventType]payloadEntry {
	return map[EventType]payloadEntry{
		EventTestsSubmitted: entry[TestsSubmitted](),
		EventTestsVerified:  entry[TestsVerified](),
		EventTestsRejected:  entry[TestsRejected](),
		EventTestsCommitted: entry[TestsCommitted](),
		EventTestsApproved:  entry[TestsApproved](),
	}
}

// TestsSubmitted is the payload of tests.submitted: the test-author handed in its tests (TECH_SPEC 7).
//
// A submission is stored whole (HARNESS_DESIGN B6): Files holds every test file, with the blob of its content.
// Refs, SHA and TreeHash are the old shape and no writer sets them now.
type TestsSubmitted struct {
	// Old events only.
	Refs  []string            `json:"refs,omitempty"`
	ACMap map[string][]string `json:"ac_map"`
	// Old events only.
	SHA id.SHA `json:"sha,omitempty"`
	// Old events only.
	TreeHash id.SHA `json:"tree_hash,omitempty"`
	// Files are the test files; Notes is what the author says about them; TaskID is the task the submission ends and
	// BaseSHA the commit it was written against.
	Files   []TestFile `json:"files,omitempty"`
	Notes   string     `json:"notes,omitempty"`
	TaskID  id.TaskID  `json:"task_id,omitempty"`
	BaseSHA id.SHA     `json:"base_sha,omitempty"`
	// ExpectedPaths are the globs outside the tests directory the change is expected to touch; the slop rule's scope
	// check reads them. Older events have none.
	ExpectedPaths []string `json:"expected_paths,omitempty"`
}

// Validate returns every problem with the payload.
func (p TestsSubmitted) Validate() error {
	var v problems
	v.each("refs", p.Refs, checkText)
	v.add("ac_map", checkACMap(p.ACMap))
	if p.SHA != "" {
		v.add("sha", checkSHA(p.SHA))
	}
	if p.TreeHash != "" {
		v.add("tree_hash", checkSHA(p.TreeHash))
	}
	v.require("files", len(p.Files) > 0 || len(p.Refs) > 0, "a submission needs files")
	v.each("files", p.Files, func(f TestFile) error { return f.validate() })
	if p.Notes != "" {
		v.add("notes", checkLongText(p.Notes))
	}
	if p.TaskID != "" {
		v.add("task_id", checkTask(p.TaskID))
	}
	if p.BaseSHA != "" {
		v.add("base_sha", checkSHA(p.BaseSHA))
	}
	v.each("expected_paths", p.ExpectedPaths, checkText)
	return v.join()
}

// TestFile is one test file of a submission: its path in the repository and the blob that holds its content.
type TestFile struct {
	Path string           `json:"path"`
	Blob workflow.BlobRef `json:"blob"`
}

func (f TestFile) validate() error {
	var v problems
	v.add("path", checkText(f.Path))
	v.add("blob", checkBlob(f.Blob))
	return v.join()
}

// TestsVerified is the payload of tests.verified: the sandbox ran the tests (red first, D4). NeedsSignoff is set by the
// engine from policy (the sensitive paths and labels, D131, D33): when true the reducer waits for tests.approved before
// the tests can be committed.
type TestsVerified struct {
	RunIDs       []id.RunID `json:"run_ids"`
	SHA          id.SHA     `json:"sha"`
	TreeHash     id.SHA     `json:"tree_hash"`
	NeedsSignoff bool       `json:"needs_signoff"`
}

// Validate returns every problem with the payload.
func (p TestsVerified) Validate() error {
	var v problems
	v.add("run_ids", checkAtLeastOne(len(p.RunIDs)))
	v.each("run_ids", p.RunIDs, checkRunID)
	v.add("sha", checkSHA(p.SHA))
	v.add("tree_hash", checkSHA(p.TreeHash))
	return v.join()
}

// TestsRejected is the payload of tests.rejected: the tests failed red-first or another check, with reasons.
type TestsRejected struct {
	Reasons []string   `json:"reasons"`
	RunIDs  []id.RunID `json:"run_ids"`
	SHA     id.SHA     `json:"sha"`
}

// Validate returns every problem with the payload.
func (p TestsRejected) Validate() error {
	var v problems
	v.add("reasons", checkAtLeastOne(len(p.Reasons)))
	v.each("reasons", p.Reasons, checkText)
	v.each("run_ids", p.RunIDs, checkRunID)
	v.add("sha", checkSHA(p.SHA))
	return v.join()
}

// TestsCommitted is the payload of tests.committed: the verified tests were committed to the ticket's branch.
type TestsCommitted struct {
	SHA      id.SHA `json:"sha"`
	TreeHash id.SHA `json:"tree_hash"`
}

// Validate returns every problem with the payload.
func (p TestsCommitted) Validate() error {
	var v problems
	v.add("sha", checkSHA(p.SHA))
	v.add("tree_hash", checkSHA(p.TreeHash))
	return v.join()
}

// TestsApproved is the payload of tests.approved: a domain approver accepted the tests of a sensitive domain (D33).
type TestsApproved struct {
	Approver ActorID `json:"approver"`
	TreeHash id.SHA  `json:"tree_hash"`
}

// Validate returns every problem with the payload.
func (p TestsApproved) Validate() error {
	var v problems
	v.add("approver", checkActor(p.Approver))
	v.add("tree_hash", checkSHA(p.TreeHash))
	return v.join()
}
