package intake

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsTestFileRecognizesPolyglotTestFiles(t *testing.T) {
	t.Parallel()
	for _, p := range []string{
		"packages/shared/src/atomicWrite.test.ts",
		"apps/web/src/Button.spec.tsx",
		"internal/api/api_test.go",
		"tests/test_refunds.py",
		"src/refunds_test.py",
		"crates/core/src/lib_test.rs",
	} {
		require.True(t, isTestFile(p), "expected %s to count as a test file", p)
	}
	for _, p := range []string{
		"packages/shared/src/atomicWrite.ts",
		"internal/api/api.go",
		"src/refunds.py",
		"README.md",
	} {
		require.False(t, isTestFile(p), "expected %s NOT to count as a test file", p)
	}
}
