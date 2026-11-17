// Package skills carries the agents' instructions inside the server binary. Each kind of work has one file here
// (workflow.Kind.Skill); the text reaches the agent through get_task, fresh for every task.
package skills

import (
	"embed"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
)

//go:embed tests.md review.md verdict.md profile.md
var files embed.FS

// For returns the instructions of a kind, or "" when the kind names no file.
func For(kind workflow.Kind) string {
	raw, err := files.ReadFile(kind.Skill)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

// All returns every skill file by name, for the start-up check of the kind table.
func All() map[string]string {
	out := map[string]string{}
	entries, err := files.ReadDir(".")
	if err != nil {
		return out
	}
	for _, e := range entries {
		if raw, err := files.ReadFile(e.Name()); err == nil {
			out[e.Name()] = string(raw)
		}
	}
	return out
}
