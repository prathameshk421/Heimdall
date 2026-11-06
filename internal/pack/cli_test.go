package pack_test

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

func TestPackHelpOffersTheDocumentedRefreshAndReviewFlow(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20_000_000_000)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "run", "../../cmd/heimdall", "pack", "--help")
	out, err := cmd.CombinedOutput()
	_ = err // The expected red is the missing help text, not the CLI's nonzero status.
	text := string(out)
	for _, phrase := range []string{"refresh", "review", "redacted", "encrypted"} {
		if !strings.Contains(strings.ToLower(text), phrase) {
			t.Errorf("pack help must describe %q; got:\n%s", phrase, text)
		}
	}
}
