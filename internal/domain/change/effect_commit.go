package change

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
)

// CommitFile is one file to commit to the branch.
type CommitFile struct {
	Path    string           `json:"path"`
	BlobRef workflow.BlobRef `json:"blob_ref"`
}

// CommitTestsPayload is the payload of a github_commit_tests effect.
type CommitTestsPayload struct {
	Repo    Repo         `json:"repo"`
	Branch  string       `json:"branch"`
	Dir     string       `json:"dir"`
	Files   []CommitFile `json:"files"`
	Message string       `json:"message"`
}

// Validate returns every problem with the payload.
func (p CommitTestsPayload) Validate() error {
	var v problems
	v.add("repo", checkRepo(p.Repo))
	if p.Branch == "" {
		v.add("branch", errors.New("is required"))
	}
	v.add("dir", finding.CheckRepoPath(p.Dir))
	v.add("files", checkAtLeastOne(len(p.Files)))
	for i, f := range p.Files {
		v.add(fmt.Sprintf("files[%d].path", i), finding.CheckRepoPath(f.Path))
		v.add(fmt.Sprintf("files[%d].blob_ref", i), checkBlob(f.BlobRef))
	}
	v.add("message", checkText(p.Message))
	return v.join()
}

// DecodeCommitTestsPayload strictly decodes and validates a github_commit_tests payload.
func DecodeCommitTestsPayload(raw json.RawMessage) (CommitTestsPayload, error) {
	var p CommitTestsPayload
	if err := decodeEffectPayload(raw, []string{"repo", "branch", "dir", "files", "message"}, &p); err != nil {
		return CommitTestsPayload{}, fmt.Errorf("github_commit_tests payload: %w", err)
	}
	return p, nil
}
