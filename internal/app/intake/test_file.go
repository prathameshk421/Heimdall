package intake

import (
	"path"
	"strings"
)

// isTestFile reports whether p is a colocated test file in Go, JS/TS, Python, or Rust, so polyglot PRs
// with unit tests (such as packages/shared/src/atomicWrite.test.ts) satisfy slop's tests_present rule.
func isTestFile(p string) bool {
	base := strings.ToLower(path.Base(p))
	for _, suffix := range []string{
		"_test.go",
		".test.ts", ".test.tsx", ".spec.ts", ".spec.tsx",
		".test.js", ".test.jsx", ".spec.js", ".spec.jsx",
		".test.mjs", ".test.cjs",
		"_test.py", "_test.rs",
	} {
		if strings.HasSuffix(base, suffix) {
			return true
		}
	}
	return strings.HasPrefix(base, "test_") && strings.HasSuffix(base, ".py")
}
