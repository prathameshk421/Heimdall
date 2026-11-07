// Package skills carries the agents' role instructions inside the server binary. The agents cannot read files, so the
// text reaches them through the get_task tool, fresh for every ticket.
package skills

import (
	"embed"
	"strings"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/workflow"
)

//go:embed test-quality/SKILL.md reviewer/SKILL.md resolver/SKILL.md
var files embed.FS

// For returns the instructions of a role, without the front matter, or "" for a role that has none.
func For(role workflow.Role) string {
	path := ""
	switch role {
	case workflow.RoleTestAuthor:
		path = "test-quality/SKILL.md"
	case workflow.RoleReviewer:
		path = "reviewer/SKILL.md"
	case workflow.RoleResolver:
		path = "resolver/SKILL.md"
	case workflow.RoleProfiler:
		return ""
	}
	if path == "" {
		return ""
	}
	raw, err := files.ReadFile(path)
	if err != nil {
		return ""
	}
	text := string(raw)
	if rest, ok := strings.CutPrefix(text, "---\n"); ok {
		if _, body, found := strings.Cut(rest, "\n---\n"); found {
			text = body
		}
	}
	return strings.TrimSpace(text)
}
