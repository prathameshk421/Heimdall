package profileyaml_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/profileyaml"
	"github.com/stretchr/testify/require"
)

var yamlFenceRE = regexp.MustCompile("(?s)```yaml\\s*\\n(.*?)\\n```")

// TestProfileSkillExamplesParseCleanly ensures every YAML example in skills/profile.md stays valid under the
// real profile parser and uses `go test -json` so examples cannot drift into D178/D180 bugs (R3).
func TestProfileSkillExamplesParseCleanly(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "skills", "profile.md"))
	require.NoError(t, err)

	matches := yamlFenceRE.FindAllStringSubmatch(string(raw), -1)
	require.NotEmpty(t, matches, "skills/profile.md must contain at least one yaml example")

	allowlist := []string{"docker.io/library/golang:1.27.3@sha256:" + strings.Repeat("a", 64)}
	for i, m := range matches {
		doc := strings.ReplaceAll(m[1], "<one image from stack_hints, exactly as written>", allowlist[0])
		p, err := profileyaml.ParseApprovedProfile([]byte(doc), allowlist)
		require.NoError(t, err, "skills/profile.md yaml example #%d failed ParseApprovedProfile", i+1)
		require.Contains(t, p.Acceptance, "-json", "skills/profile.md yaml example #%d acceptance command must emit JSON test events", i+1)
	}
}
