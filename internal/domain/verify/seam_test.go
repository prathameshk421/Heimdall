package verify_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/verify"
)

const (
	appPkg      = "github.com/aarush323/heimdall-fixture-pay/internal/app"
	payInternal = "github.com/aarush323/heimdall-fixture-pay/internal"
	testPath    = "tests/heimdall/PAY-1/refunds_test.go"
)

// A test file that reaches into an internal package of the system under test is a seam violation. The line is the
// line of the import path, which is what a reviewer wants to open.
func TestSeamFindsADirectInternalImport(t *testing.T) {
	src := `package refunds_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/aarush323/heimdall-fixture-pay/internal/app"
)
`
	got, err := verify.SeamImports(map[string]string{testPath: src}, []string{appPkg})
	require.NoError(t, err)
	require.Equal(t, []verify.Import{{File: testPath, Line: 8, Path: appPkg}}, got)
}

// A prefix covers the package and everything under it, and nothing that merely starts with the same letters.
func TestSeamMatchesThePrefixAndItsSubpackagesOnly(t *testing.T) {
	src := `package ledger_test

import (
	_ "github.com/aarush323/heimdall-fixture-pay/internal/app/intake"
	. "github.com/aarush323/heimdall-fixture-pay/internal/application"
	app "github.com/aarush323/heimdall-fixture-pay/internal/app"
)
`
	got, err := verify.SeamImports(map[string]string{testPath: src}, []string{appPkg})
	require.NoError(t, err)
	require.Equal(t, []verify.Import{
		{File: testPath, Line: 4, Path: appPkg + "/intake"},
		{File: testPath, Line: 6, Path: appPkg},
	}, got)
}

// The whole internal tree can be forbidden with one prefix.
func TestSeamForbidsAWholeInternalTree(t *testing.T) {
	src := `package refunds_test

import (
	"github.com/aarush323/heimdall-fixture-pay/internal/gate"
	"github.com/aarush323/heimdall-fixture-pay/internal/domain/policy"
)
`
	got, err := verify.SeamImports(map[string]string{testPath: src}, []string{payInternal})
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, "github.com/aarush323/heimdall-fixture-pay/internal/gate", got[0].Path)
	require.Equal(t, "github.com/aarush323/heimdall-fixture-pay/internal/domain/policy", got[1].Path)
}

// Only test files are checked; the product's own files may import its internals.
func TestSeamIgnoresNonTestFiles(t *testing.T) {
	src := `package refunds

import "github.com/aarush323/heimdall-fixture-pay/internal/app"

var handler = app.New
`
	got, err := verify.SeamImports(map[string]string{"refunds/refunds.go": src}, []string{appPkg})
	require.NoError(t, err)
	require.Empty(t, got)
}

// Imports that a test may use (the standard library, testify) are not violations.
func TestSeamAllowsTheSeamAndTheStandardLibrary(t *testing.T) {
	src := `package refunds_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)
`
	got, err := verify.SeamImports(map[string]string{testPath: src}, []string{appPkg})
	require.NoError(t, err)
	require.Empty(t, got)
}

// An empty forbidden list forbids nothing, so the check cannot pass by accident when the profile is silent.
func TestSeamWithNoForbiddenPrefixesFindsNothing(t *testing.T) {
	src := `package refunds_test

import "github.com/aarush323/heimdall-fixture-pay/internal/app"
`
	got, err := verify.SeamImports(map[string]string{testPath: src}, nil)
	require.NoError(t, err)
	require.Empty(t, got)
}

// An import written inside a comment, a string or a raw string is not an import.
func TestSeamIgnoresCommentsAndStrings(t *testing.T) {
	src := "package refunds_test\n\n" +
		"// import \"github.com/aarush323/heimdall-fixture-pay/internal/app\"\n" +
		"/* import (\n\t\"github.com/aarush323/heimdall-fixture-pay/internal/app\"\n) */\n" +
		"const note = \"import \\\"github.com/aarush323/heimdall-fixture-pay/internal/app\\\"\"\n" +
		"const raw = `import \"github.com/aarush323/heimdall-fixture-pay/internal/app\"`\n"
	got, err := verify.SeamImports(map[string]string{testPath: src}, []string{appPkg})
	require.NoError(t, err)
	require.Empty(t, got)
}

// cgo's pseudo-import is skipped, and a file with no imports is fine.
func TestSeamHandlesFilesWithoutImports(t *testing.T) {
	withCgo := "package refunds_test\n\n/*\n#include <stdlib.h>\n*/\nimport \"C\"\n"
	got, err := verify.SeamImports(map[string]string{testPath: withCgo}, []string{appPkg})
	require.NoError(t, err)
	require.Empty(t, got)

	got, err = verify.SeamImports(map[string]string{testPath: "package refunds_test\n"}, []string{appPkg})
	require.NoError(t, err)
	require.Empty(t, got)
}

// Files are reported in path order so the message is stable whatever order the map yields.
func TestSeamReportsFilesInPathOrder(t *testing.T) {
	bad := "package p_test\n\nimport \"github.com/aarush323/heimdall-fixture-pay/internal/app\"\n"
	files := map[string]string{
		"tests/heimdall/PAY-2/b_test.go": bad,
		"tests/heimdall/PAY-1/a_test.go": bad,
		"tests/heimdall/PAY-1/c_test.go": bad,
	}
	got, err := verify.SeamImports(files, []string{appPkg})
	require.NoError(t, err)
	require.Equal(t, []verify.Import{
		{File: "tests/heimdall/PAY-1/a_test.go", Line: 3, Path: appPkg},
		{File: "tests/heimdall/PAY-1/c_test.go", Line: 3, Path: appPkg},
		{File: "tests/heimdall/PAY-2/b_test.go", Line: 3, Path: appPkg},
	}, got)
}

// A file the scanner cannot read is an error, never a silent pass.
func TestSeamRefusesAFileItCannotScan(t *testing.T) {
	for name, src := range map[string]string{
		"unclosed import block": "package refunds_test\n\nimport (\n\t\"testing\"\n",
		"unterminated string":   "package refunds_test\n\nimport \"github.com/aarush323/heimdall-fixture-pay/internal/app\n",
		"unterminated comment":  "package refunds_test\n\n/* import \"x\"\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := verify.SeamImports(map[string]string{testPath: src}, []string{appPkg})
			require.Error(t, err)
			require.Contains(t, err.Error(), testPath)
		})
	}
}

// Imports are reported in the order they appear, and a build-tagged test file is still a test file.
func TestSeamReportsImportsInSourceOrder(t *testing.T) {
	src := `//go:build e2e

package refunds_test

import (
	"github.com/aarush323/heimdall-fixture-pay/internal/gate"
	"github.com/aarush323/heimdall-fixture-pay/internal/app"
)
`
	got, err := verify.SeamImports(map[string]string{testPath: src}, []string{payInternal})
	require.NoError(t, err)
	require.Equal(t, []verify.Import{
		{File: testPath, Line: 6, Path: "github.com/aarush323/heimdall-fixture-pay/internal/gate"},
		{File: testPath, Line: 7, Path: "github.com/aarush323/heimdall-fixture-pay/internal/app"},
	}, got)
}
