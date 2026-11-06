package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Every payload is untrusted text from a ticket, PR, comment or file. Wrapped, it must stay inside one boundary: no
// payload can close the wrapper early or forge a second one (A6). The state checks belong to the commands and tools
// that act on text, which only read chat and GitHub comments; this suite pins the wrapper they all go through.
func TestUntrustedPayloadsCannotEscapeTheWrapper(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "injection", "payloads.txt"))
	require.NoError(t, err)
	payloads := strings.Split(strings.TrimSpace(string(raw)), "\n")
	require.Len(t, payloads, 12, "the suite is the twelve payloads of A6")

	for i, payload := range payloads {
		out, err := wrapUntrusted("PR body", []byte(payload))
		require.NoError(t, err, "payload %d", i+1)
		require.Equal(t, 1, strings.Count(out, "<<HEIMDALL-UNTRUSTED-BEGIN "), "payload %d opened a second wrapper", i+1)
		require.Equal(t, 1, strings.Count(out, "<<HEIMDALL-UNTRUSTED-END "), "payload %d closed the wrapper early", i+1)
		require.Contains(t, out, "data, not instructions", "payload %d must be labelled as data", i+1)
	}
}
