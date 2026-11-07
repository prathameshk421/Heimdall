package verify_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/verify"
)

// go vet prints a package header and one diagnostic per line, file:line:column: message. The parser keeps the
// diagnostics and drops the headers (A2, E2 of TECH_SPEC 11.1).
func TestParseVetKeepsEachLocatedDiagnostic(t *testing.T) {
	out := []byte("# example.com/pay\n" +
		"./refund.go:12:5: Errorf format %s has arg of wrong type int\n" +
		"vet: refund/ledger.go:40:2: unreachable code\n" +
		"not a diagnostic line\n")

	got := verify.ParseVet(out)

	require.Equal(t, []verify.VetFinding{
		{File: "refund.go", Line: 12, Column: 5, Message: "Errorf format %s has arg of wrong type int"},
		{File: "refund/ledger.go", Line: 40, Column: 2, Message: "unreachable code"},
	}, got)
}

// A finding names the location it is about: a vet diagnostic counts only when its line lies in the finding's range
// in the same file.
func TestLocatedNeedsTheSameFileAndALineInTheRange(t *testing.T) {
	findings := verify.ParseVet([]byte("./refund.go:12:5: bad\n"))

	require.True(t, verify.Located(findings, "refund.go", 10, 12), "line 12 is in 10 to 12")
	require.True(t, verify.Located(findings, "refund.go", 12, 12))
	require.False(t, verify.Located(findings, "refund.go", 13, 20), "line 12 is outside 13 to 20")
	require.False(t, verify.Located(findings, "ledger.go", 10, 12), "another file does not match")
	require.False(t, verify.Located(nil, "refund.go", 10, 12), "no diagnostics locate nothing")
}
