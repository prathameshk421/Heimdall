package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
)

// Orientation: what an agent needs before its first file read, in get_task itself. On live KAN-24 the test author
// spent 4 calls reading the ticket one blob at a time and 11 more exploring the repository folder by folder, about 9
// model turns, before it could start. These fields give it the ticket text and a map of the repository at once.

// maxMapLines bounds the repository map. Tests of other tickets fold into one line each.
const maxMapLines = 400

// repoMap lists the files of the repository at the base commit, one per line, with each other ticket's tests folded
// into a single line. It is "" when no file list is configured or it cannot be read.
func (s *Server) repoMap(ctx context.Context, a Assignment) string {
	if s.deps.Paths == nil || a.Task.Change.BaseSHA == "" {
		return ""
	}
	paths, err := s.deps.Paths.Paths(ctx, change.Repo(a.Task.Change.Repo), a.Task.Change.BaseSHA)
	if err != nil {
		s.deps.Logger.WarnContext(ctx, "repository map unavailable", "ticket", string(a.Task.Change.ID), "error", err)
		return ""
	}
	return foldMap(paths, string(a.Task.Change.ID))
}

// foldMap keeps every path except the tests of other tickets, which become "tests/heimdall/KAN-9/ (2 files)".
func foldMap(paths []string, ticket string) string {
	var lines []string
	folded := map[string]int{}
	var order []string
	for _, p := range paths {
		parts := strings.SplitN(p, "/", 4)
		if len(parts) >= 3 && parts[0] == "tests" && parts[1] == "heimdall" && parts[2] != ticket {
			dir := strings.Join(parts[:3], "/") + "/"
			if folded[dir] == 0 {
				order = append(order, dir)
			}
			folded[dir]++
			continue
		}
		lines = append(lines, p)
	}
	for _, dir := range order {
		lines = append(lines, fmt.Sprintf("%s (%d files of another ticket)", dir, folded[dir]))
	}
	if len(lines) > maxMapLines {
		lines = append(lines[:maxMapLines], fmt.Sprintf("... %d more", len(lines)-maxMapLines))
	}
	return strings.Join(lines, "\n")
}

// rootRefusal is the answer to read_code on the repository root when there is no map: what to do instead, per role.
func rootRefusal(role workflow.Role) error {
	if role == workflow.RoleTestAuthor {
		return refusal("Name a file or folder, for example internal/api. A folder lists its files.")
	}
	return refusal("Name a file or folder, for example internal/api. files_changed in get_task names what the change touches.")
}

// maxReadPaths bounds one read_code call with several paths.
const maxReadPaths = 8

// readCodeMany reads several files in one call, each in its own untrusted block; a missing one is named, not fatal.
func (s *Server) readCodeMany(ctx context.Context, p Principal, paths []string, sha string) (string, error) {
	if len(paths) > maxReadPaths {
		return "", refusalf("paths: at most %d files per call, not %d. Example: \"paths\": [\"internal/api/api.go\", \"internal/app/app.go\"].", maxReadPaths, len(paths))
	}
	var out []string
	for _, path := range paths {
		args, err := json.Marshal(map[string]string{"path": path, "sha": sha})
		if err != nil {
			return "", err
		}
		text, err := s.readCode(ctx, p, args)
		var ref refusal
		if errors.As(err, &ref) {
			text = path + ": " + string(ref)
		} else if err != nil {
			return "", err
		}
		out = append(out, text)
	}
	return strings.Join(out, "\n\n"), nil
}

// readRoot answers read_code on the repository root with the repository map.
func (s *Server) readRoot(ctx context.Context, p Principal) (string, error) {
	a, err := s.currentTask(ctx, p)
	if err != nil {
		return "", err
	}
	if m := s.repoMap(ctx, a); m != "" {
		return wrapUntrusted("every file at the base commit", []byte(m))
	}
	return "", rootRefusal(p.Role)
}
