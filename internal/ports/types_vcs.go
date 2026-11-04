package ports

import (
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
)

// PRState is the GitHub state of a pull request.
type PRState string

// The pull request states.
const (
	PRStateOpen   PRState = "open"
	PRStateClosed PRState = "closed"
)

// PR is a pull request as the VCS reports it. Title, Body and Branch are untrusted text (the slop rule reads them,
// and only the rule reads them).
type PR struct {
	Repo    change.Repo    `json:"repo"`
	Number  int            `json:"number"`
	Title   string         `json:"title"`
	Body    string         `json:"body"`
	Branch  string         `json:"branch"`
	Author  change.ActorID `json:"author"`
	BaseSHA id.SHA         `json:"base_sha"`
	HeadSHA id.SHA         `json:"head_sha"`
	State   PRState        `json:"state"`
	Merged  bool           `json:"merged"`
}

// Validate returns every problem with the pull request, each naming its field.
func (p PR) Validate() error {
	var v problems
	v.add("repo", checkRepo(p.Repo))
	v.require("number", p.Number >= 1, "must be 1 or more")
	v.add("title", checkText(p.Title, MaxUntrustedText))
	v.require("body", len(p.Body) <= MaxUntrustedText, "is longer than the untrusted text bound")
	v.add("branch", checkText(p.Branch, 255))
	v.add("author", checkActor(p.Author))
	v.add("base_sha", checkSHA(p.BaseSHA))
	v.add("head_sha", checkSHA(p.HeadSHA))
	v.require("state", p.State == PRStateOpen || p.State == PRStateClosed, "must be open or closed")
	return v.join()
}

// FileStatus is what happened to a file in a change.
type FileStatus string

// The file statuses a diff can report.
const (
	FileAdded    FileStatus = "added"
	FileModified FileStatus = "modified"
	FileRemoved  FileStatus = "removed"
	FileRenamed  FileStatus = "renamed"
)

// ChangedFile is one entry of a pull request's diff.
type ChangedFile struct {
	Path      string     `json:"path"`
	Status    FileStatus `json:"status"`
	Additions int        `json:"additions"`
	Deletions int        `json:"deletions"`
}

// Validate returns every problem with the entry.
func (f ChangedFile) Validate() error {
	var v problems
	v.add("path", checkRelPath(f.Path))
	switch f.Status {
	case FileAdded, FileModified, FileRemoved, FileRenamed:
	default:
		v.require("status", false, "must be added, modified, removed or renamed")
	}
	v.require("additions", f.Additions >= 0, "must not be negative")
	v.require("deletions", f.Deletions >= 0, "must not be negative")
	return v.join()
}

// File is content to write into a branch.
type File struct {
	Path    string `json:"path"`
	Content []byte `json:"content"`
}

// Validate returns every problem with the file.
func (f File) Validate() error {
	var v problems
	v.add("path", checkRelPath(f.Path))
	return v.join()
}

// CheckStatus is where a check run is in its life.
type CheckStatus string

// The check run statuses.
const (
	CheckQueued     CheckStatus = "queued"
	CheckInProgress CheckStatus = "in_progress"
	CheckCompleted  CheckStatus = "completed"
)

// Check is the state of one check run on a commit (the required heimdall/gate check, TECH_SPEC 13). ExternalID lets a
// later call update the same run (TECH_SPEC 2.4).
type Check struct {
	Name       string              `json:"name"`
	Status     CheckStatus         `json:"status"`
	Conclusion change.CIConclusion `json:"conclusion,omitempty"`
	ExternalID string              `json:"external_id,omitempty"`
	Summary    string              `json:"summary"`
}

// Validate returns every problem with the check. A conclusion is required once the run is completed, and refused
// before that.
func (c Check) Validate() error {
	var v problems
	v.add("name", checkText(c.Name, 255))
	switch c.Status {
	case CheckQueued, CheckInProgress:
		v.require("conclusion", c.Conclusion == "", "must be empty until the check completes")
	case CheckCompleted:
		v.require("conclusion", c.Conclusion.Valid(), "must be set when the check completes")
	default:
		v.require("status", false, "must be queued, in_progress or completed")
	}
	return v.join()
}
