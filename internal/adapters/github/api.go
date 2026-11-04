package github

import (
	"fmt"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// IssueComment is one comment on a pull request's conversation. UserType is GitHub's account type ("User", "Bot").
type IssueComment struct {
	ID       int64
	Login    string
	UserType string
	Body     string
}

// Review is one submitted pull request review. CommitID is the commit it was submitted on. Approvals count only on
// the head commit (TECH_SPEC 12.4), so the gate compares it with the head.
type Review struct {
	ID       int64
	Login    string
	State    string
	CommitID id.SHA
}

// prJSON is the part of a pull request the adapter reads.
type prJSON struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	Body   string `json:"body"`
	State  string `json:"state"`
	Merged bool   `json:"merged"`
	User   struct {
		Login string `json:"login"`
	} `json:"user"`
	Head struct {
		Ref string `json:"ref"`
		SHA string `json:"sha"`
	} `json:"head"`
	Base struct {
		SHA string `json:"sha"`
	} `json:"base"`
}

// toPR maps a pull request and validates it against the port's bounds, so an oversized body is refused here.
func (p prJSON) toPR(repo change.Repo) (ports.PR, error) {
	pr := ports.PR{
		Repo:    repo,
		Number:  p.Number,
		Title:   p.Title,
		Body:    p.Body,
		Branch:  p.Head.Ref,
		Author:  change.ActorID(p.User.Login),
		BaseSHA: id.SHA(p.Base.SHA),
		HeadSHA: id.SHA(p.Head.SHA),
		State:   ports.PRState(p.State),
		Merged:  p.Merged,
	}
	if err := pr.Validate(); err != nil {
		return ports.PR{}, fmt.Errorf("github: pull request %d: %w", p.Number, err)
	}
	return pr, nil
}

// fileJSON is one entry of a pull request's file list.
type fileJSON struct {
	Filename  string `json:"filename"`
	Status    string `json:"status"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
}

// toFile maps one file. Filename is the new path, which is the path a rename lands on.
func (f fileJSON) toFile() (ports.ChangedFile, error) {
	status, err := fileStatus(f.Status)
	if err != nil {
		return ports.ChangedFile{}, fmt.Errorf("github: file %q: %w", f.Filename, err)
	}
	cf := ports.ChangedFile{Path: f.Filename, Status: status, Additions: f.Additions, Deletions: f.Deletions}
	if err := cf.Validate(); err != nil {
		return ports.ChangedFile{}, fmt.Errorf("github: file %q: %w", f.Filename, err)
	}
	return cf, nil
}

// fileStatus maps GitHub's file status. An unknown one is an error, never a guess.
func fileStatus(s string) (ports.FileStatus, error) {
	switch s {
	case "added":
		return ports.FileAdded, nil
	case "modified", "changed":
		return ports.FileModified, nil
	case "removed":
		return ports.FileRemoved, nil
	case "renamed":
		return ports.FileRenamed, nil
	}
	return "", fmt.Errorf("unknown file status %q", s)
}

// reviewJSON is one review as GitHub returns it.
type reviewJSON struct {
	ID       int64  `json:"id"`
	State    string `json:"state"`
	CommitID string `json:"commit_id"`
	User     struct {
		Login string `json:"login"`
	} `json:"user"`
}

// commentJSON is one issue comment as GitHub returns it.
type commentJSON struct {
	ID   int64  `json:"id"`
	Body string `json:"body"`
	User struct {
		Login string `json:"login"`
		Type  string `json:"type"`
	} `json:"user"`
}

// refJSON is the object a git ref points at.
type refJSON struct {
	Object struct {
		SHA string `json:"sha"`
	} `json:"object"`
}

// repoJSON is the part of a repository the adapter reads: the name of its default branch.
type repoJSON struct {
	DefaultBranch string `json:"default_branch"`
}

// commitJSON is the tree of a git commit.
type commitJSON struct {
	Tree struct {
		SHA string `json:"sha"`
	} `json:"tree"`
}

// shaJSON is any object that answers with its own SHA.
type shaJSON struct {
	SHA string `json:"sha"`
}

// treeJSON is a git tree listing.
type treeJSON struct {
	Tree []struct {
		Path string `json:"path"`
		Type string `json:"type"`
		SHA  string `json:"sha"`
	} `json:"tree"`
}

// treeEntry is one entry of a tree the adapter writes.
type treeEntry struct {
	Path string `json:"path"`
	Mode string `json:"mode"`
	Type string `json:"type"`
	SHA  string `json:"sha"`
}

// checkRunsJSON is the check-run listing of one commit, filtered by name.
type checkRunsJSON struct {
	CheckRuns []struct {
		ID         int64  `json:"id"`
		ExternalID string `json:"external_id"`
	} `json:"check_runs"`
}

// checkBody is the body of a check run create or update. Empty fields are left out, so an update does not reset them.
type checkBody struct {
	Name       string      `json:"name,omitempty"`
	HeadSHA    string      `json:"head_sha,omitempty"`
	Status     string      `json:"status"`
	Conclusion string      `json:"conclusion,omitempty"`
	ExternalID string      `json:"external_id,omitempty"`
	Output     checkOutput `json:"output"`
}

type checkOutput struct {
	Title   string `json:"title"`
	Summary string `json:"summary"`
}
