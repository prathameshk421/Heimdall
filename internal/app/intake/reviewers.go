package intake

import (
	"context"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/id"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/policy"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

func codeownersPaths() []string {
	return []string{".github/CODEOWNERS", "CODEOWNERS", "docs/CODEOWNERS"}
}

func (in *Intake) reviewerSuggestions(ctx context.Context, repo change.Repo, base id.SHA, files []ports.ChangedFile) ([]change.ActorID, error) {
	read, err := readPolicy(ctx, in.code, in.policy, repo, base)
	if err != nil {
		return nil, err
	}
	if read.invalid != "" {
		return nil, nil
	}
	contents, err := in.readCodeowners(ctx, repo, base)
	if err != nil || contents == nil {
		return nil, err
	}
	owners := codeownersForFiles(string(contents), files)
	return reviewerAgents(owners, read.pol), nil
}

func (in *Intake) readCodeowners(ctx context.Context, repo change.Repo, base id.SHA) ([]byte, error) {
	var contents []byte
	for _, candidate := range codeownersPaths() {
		var err error
		contents, err = in.code.ReadFile(ctx, repo, base, candidate)
		if errors.Is(err, ports.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read CODEOWNERS at %s: %w", candidate, err)
		}
		return contents, nil
	}
	return contents, nil
}

func reviewerAgents(owners []string, pol policy.Policy) []change.ActorID {
	peopleByLogin := make(map[string]string, len(pol.People))
	peopleByTeam := make(map[string][]string)
	for _, entry := range pol.People {
		if entry.GitHub != "" && entry.Agent != "" {
			peopleByLogin[strings.ToLower(entry.GitHub)] = entry.Agent
		}
		if entry.Agent != "" {
			for _, team := range entry.Teams {
				key := strings.ToLower(team)
				peopleByTeam[key] = append(peopleByTeam[key], entry.Agent)
			}
		}
	}
	var selected []change.ActorID
	for _, owner := range owners {
		key := strings.ToLower(strings.TrimPrefix(owner, "@"))
		if agent := peopleByLogin[key]; agent != "" {
			if candidate := change.ActorID(agent); !slices.Contains(selected, candidate) {
				selected = append(selected, candidate)
			}
			continue
		}
		team := key
		if _, after, ok := strings.Cut(key, "/"); ok {
			team = after
		}
		for _, agent := range peopleByTeam[team] {
			candidate := change.ActorID(agent)
			if !slices.Contains(selected, candidate) {
				selected = append(selected, candidate)
			}
		}
	}
	return selected
}

func codeownersForFiles(contents string, files []ports.ChangedFile) []string {
	owners := make([]string, 0)
	seen := make(map[string]bool)
	for _, file := range files {
		var matching []string
		for _, line := range strings.Split(contents, "\n") {
			line = strings.TrimSpace(strings.SplitN(line, "#", 2)[0])
			fields := strings.Fields(line)
			if len(fields) < 2 || !codeownersMatch(fields[0], file.Path) {
				continue
			}
			matching = fields[1:]
		}
		for _, owner := range matching {
			if !seen[strings.ToLower(owner)] {
				seen[strings.ToLower(owner)] = true
				owners = append(owners, owner)
			}
		}
	}
	return owners
}

func codeownersMatch(pattern, file string) bool {
	pattern = strings.TrimPrefix(pattern, "/")
	if strings.HasSuffix(pattern, "/") {
		pattern += "**"
	}
	parts := strings.Split(pattern, "/")
	pathParts := strings.Split(strings.TrimPrefix(file, "/"), "/")
	if !strings.Contains(pattern, "/") {
		for i := range pathParts {
			if codeownerSegmentsMatch(parts, pathParts[i:]) {
				return true
			}
		}
		return false
	}
	return codeownerSegmentsMatch(parts, pathParts)
}

func codeownerSegmentsMatch(pattern, file []string) bool {
	if len(pattern) == 0 {
		return len(file) == 0
	}
	if pattern[0] == "**" {
		for i := 0; i <= len(file); i++ {
			if codeownerSegmentsMatch(pattern[1:], file[i:]) {
				return true
			}
		}
		return false
	}
	if len(file) == 0 {
		return false
	}
	matched, err := path.Match(pattern[0], file[0])
	return err == nil && matched && codeownerSegmentsMatch(pattern[1:], file[1:])
}
