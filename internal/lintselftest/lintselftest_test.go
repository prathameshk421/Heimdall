//go:build lintselftest

package lintselftest_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

type fixture struct {
	name  string
	path  string
	wants []want
	body  []byte
}

type want struct{ linter, text string }

type issue struct {
	FromLinter string
	Text       string
	Pos        struct {
		Filename string
		Line     int
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	require.NoError(t, err)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		require.NotEqual(t, dir, parent, "go.mod not found above the test directory")
		dir = parent
	}
}

func loadFixtures(t *testing.T, dir string) []fixture {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	require.NoError(t, err)
	require.NotEmpty(t, files, "no fixtures in %s", dir)
	var out []fixture
	for _, f := range files {
		body, err := os.ReadFile(f) // #nosec G304 -- paths come from a glob of the repo's own testdata
		require.NoError(t, err)
		fx := fixture{name: filepath.Base(f), body: body}
		for _, line := range strings.Split(string(body), "\n") {
			rest, ok := strings.CutPrefix(line, "// lintselftest: ")
			if !ok {
				continue
			}
			key, val, _ := strings.Cut(rest, "=")
			switch key {
			case "path":
				fx.path = val
			case "want":
				linter, text, _ := strings.Cut(val, "|")
				fx.wants = append(fx.wants, want{linter: linter, text: text})
			default:
				t.Fatalf("%s: unknown header %q", f, key)
			}
		}
		require.NotEmpty(t, fx.path, "%s has no path header", f)
		out = append(out, fx)
	}
	return out
}

// stubs are the packages fixtures import to cross layer boundaries.
var stubs = map[string]string{
	"internal/testkit/testkit.go":     "// Package testkit is a stub.\npackage testkit\n\n// Stub is a stub.\ntype Stub struct{}\n",
	"internal/ports/ports.go":         "// Package ports is a stub.\npackage ports\n\n// Stub is a stub.\ntype Stub struct{}\n",
	"internal/domain/stub/stub.go":    "// Package stub is a stub.\npackage stub\n\n// Stub is a stub.\ntype Stub struct{}\n",
	"internal/app/stub/stub.go":       "// Package stub is a stub.\npackage stub\n\n// Stub is a stub.\ntype Stub struct{}\n",
	"internal/adapters/stub/stub.go":  "// Package stub is a stub.\npackage stub\n\n// Stub is a stub.\ntype Stub struct{}\n",
	"internal/transport/stub/stub.go": "// Package stub is a stub.\npackage stub\n\n// Stub is a stub.\ntype Stub struct{}\n",
}

func golangciBinary(t *testing.T, root string) string {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "go", "tool", "-modfile=tools.mod", "-n", "golangci-lint")
	cmd.Dir = root
	out, err := cmd.Output()
	require.NoError(t, err, "build the pinned golangci-lint")
	return strings.TrimSpace(string(out))
}

// lint writes the fixtures into a temporary module that shares the repo's go.mod, go.sum and
// .golangci.yml, runs golangci-lint with every de-duplication off, and returns the findings by file.
func lint(t *testing.T, root string, fixtures []fixture) map[string][]issue {
	t.Helper()
	tmp := t.TempDir()
	for _, name := range []string{"go.mod", "go.sum", ".golangci.yml"} {
		b, err := os.ReadFile(filepath.Join(root, name)) // #nosec G304 -- fixed names under the repo root
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(tmp, name), b, 0o600))
	}
	put := func(rel string, body []byte) {
		p := filepath.Join(tmp, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o750))
		require.NoError(t, os.WriteFile(p, body, 0o600))
	}
	for rel, body := range stubs {
		put(rel, []byte(body))
	}
	for _, fx := range fixtures {
		put(fx.path, fx.body)
	}
	jsonOut := filepath.Join(tmp, "out.json")
	cmd := exec.CommandContext(context.Background(), golangciBinary(t, root), "run",
		"--uniq-by-line=false", "--max-same-issues=0", "--max-issues-per-linter=0",
		"--output.json.path="+jsonOut, "--output.text.path=", "./...")
	cmd.Dir = tmp
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	// Exit status 1 means "findings", which is what the bad fixtures are for; anything else is a failure to run.
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		require.ErrorAs(t, err, &ee, stderr.String())
		require.Equal(t, 1, ee.ExitCode(), stderr.String())
	}
	raw, err := os.ReadFile(jsonOut) // #nosec G304 -- file we just asked golangci-lint to write
	require.NoError(t, err)
	var report struct{ Issues []issue }
	require.NoError(t, json.Unmarshal(raw, &report))
	by := map[string][]issue{}
	for _, is := range report.Issues {
		by[filepath.ToSlash(is.Pos.Filename)] = append(by[filepath.ToSlash(is.Pos.Filename)], is)
	}
	return by
}

func TestEveryBadFixtureTriggersItsRules(t *testing.T) {
	root := repoRoot(t)
	fixtures := loadFixtures(t, filepath.Join(root, "testdata/lint/bad"))
	by := lint(t, root, fixtures)
	for _, fx := range fixtures {
		t.Run(fx.name, func(t *testing.T) {
			require.NotEmpty(t, fx.wants, "a bad fixture must say which rule it breaks")
			for _, w := range fx.wants {
				found := false
				for _, is := range by[fx.path] {
					if is.FromLinter == w.linter && strings.Contains(is.Text, w.text) {
						found = true
					}
				}
				require.True(t, found, "%s: expected %s finding containing %q, got %s", fx.path, w.linter, w.text, describe(by[fx.path]))
			}
		})
	}
}

func TestGoodFixturesProduceNoFindings(t *testing.T) {
	root := repoRoot(t)
	fixtures := loadFixtures(t, filepath.Join(root, "testdata/lint/good"))
	by := lint(t, root, fixtures)
	for _, fx := range fixtures {
		require.Empty(t, by[fx.path], "%s should be clean: %s", fx.path, describe(by[fx.path]))
	}
}

// TestNoRuleIsWithoutAFixture fails when a linter, a depguard rule or a forbidigo pattern is added to
// .golangci.yml without a fixture, so the self-test cannot fall behind the configuration.
func TestNoRuleIsWithoutAFixture(t *testing.T) {
	root := repoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, ".golangci.yml")) // #nosec G304 -- fixed name under the repo root
	require.NoError(t, err)
	var cfg struct {
		Linters struct {
			Enable   []string
			Settings struct {
				Depguard  struct{ Rules map[string]yaml.Node }
				Forbidigo struct {
					Forbid []struct{ Pattern, Msg string }
				}
			}
		}
		Formatters struct{ Enable []string }
	}
	require.NoError(t, yaml.Unmarshal(raw, &cfg))

	covered := map[string]bool{}
	var texts []string
	for _, fx := range loadFixtures(t, filepath.Join(root, "testdata/lint/bad")) {
		for _, w := range fx.wants {
			covered[w.linter] = true
			texts = append(texts, w.linter+"|"+w.text)
		}
	}
	needed := append(append([]string{}, cfg.Linters.Enable...), cfg.Formatters.Enable...)
	for _, l := range needed {
		require.True(t, covered[l], "no bad fixture for linter %s", l)
	}
	hasText := func(linter, sub string) bool {
		for _, tx := range texts {
			if strings.HasPrefix(tx, linter+"|") && strings.Contains(tx, sub) {
				return true
			}
		}
		return false
	}
	var rules []string
	for name := range cfg.Linters.Settings.Depguard.Rules {
		rules = append(rules, name)
	}
	sort.Strings(rules)
	for _, name := range rules {
		require.True(t, hasText("depguard", "'"+name+"'"), "no bad fixture for depguard rule %s", name)
	}
	for _, f := range cfg.Linters.Settings.Forbidigo.Forbid {
		for _, ident := range identifiers(f.Pattern) {
			require.True(t, hasText("forbidigo", "`"+ident+"`"), "no bad fixture for forbidigo pattern %s (looked for %s)", f.Pattern, ident)
		}
	}
}

// identifiers turns a simple forbidigo pattern such as ^fmt\.(Print|Printf)$ into every name it forbids
// ("fmt.Print", "fmt.Printf") so each alternative has its own fixture: a typo in one alternative must not hide.
func identifiers(pattern string) []string {
	p := strings.NewReplacer("^", "", "$", "", `\.`, ".").Replace(pattern)
	head, alts, grouped := strings.Cut(p, "(")
	if !grouped {
		return []string{p}
	}
	var out []string
	for _, a := range strings.Split(strings.TrimSuffix(alts, ")"), "|") {
		out = append(out, head+a)
	}
	return out
}

func describe(is []issue) string {
	var parts []string
	for _, i := range is {
		parts = append(parts, fmt.Sprintf("%s:%d %s", i.FromLinter, i.Pos.Line, i.Text))
	}
	return "[" + strings.Join(parts, "; ") + "]"
}
