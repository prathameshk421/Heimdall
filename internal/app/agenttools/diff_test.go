package agenttools

import (
	"strings"
	"testing"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

func TestRenderDiffShowsEveryFileAndSaysWhenAPatchIsMissing(t *testing.T) {
	got := renderDiff([]ports.ChangedFile{
		{Path: "internal/api/api.go", Status: "modified", Additions: 1, Deletions: 0, Patch: "@@ -1 +1,2 @@\n+added"},
		{Path: "big.bin", Status: "added"},
	})
	for _, want := range []string{"=== internal/api/api.go (modified, +1 -0)", "+added", "=== big.bin", "no patch"} {
		if !strings.Contains(got, want) {
			t.Errorf("diff has no %q:\n%s", want, got)
		}
	}
}
