package schema_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/deliberation"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"
)

// toolName matches a tool name as an agent writes it. It needs a letter after the prefix, so the prose form
// "heimdall_*" is not mistaken for a tool called "heimdall_".
var toolName = regexp.MustCompile(`heimdall_[a-z][a-z_]*`)

// placeholder matches the words a skill must never carry into a run.
var placeholder = regexp.MustCompile(`(?i)\b(todo|tbd)\b`)

// readText reads a file the skills test needs and fails the test when it is missing.
func readText(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err, "read %s", path)
	return string(b)
}

// skillBody returns one skill's SKILL.md.
func skillBody(t *testing.T, skill string) string {
	t.Helper()
	return readText(t, filepath.Join("..", "..", "skills", skill, "SKILL.md"))
}

// exampleFiles returns the JSON examples of one skill, and insists there are at least three of them.
func exampleFiles(t *testing.T, skill string) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("..", "..", "skills", skill, "examples", "*.json"))
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(files), 3, "skills/%s/examples needs at least 3 JSON examples", skill)
	return files
}

// decodeExample decodes one example the way the MCP server decodes an agent's submission: an undeclared field is an
// error, not something silently dropped. It returns the raw bytes for the schema check.
func decodeExample(t *testing.T, file string, v any) []byte {
	t.Helper()
	b, err := os.ReadFile(file)
	require.NoError(t, err)
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	require.NoError(t, dec.Decode(v), "%s does not decode as its contract", file)
	return b
}

// fitsTopLevel applies the check the package's own golden test uses: every key is a declared property of the schema,
// every required property is present, and every enum field holds a listed value. It is a structural check, not JSON
// Schema validation: no validator is in go.sum (see doc.go). The domain Validate methods carry the rest.
func fitsTopLevel(t *testing.T, id string, b []byte) {
	t.Helper()
	var inst map[string]any
	require.NoError(t, json.Unmarshal(b, &inst))
	m := mustSchema(t, id)
	props := at(t, m, "properties")
	req, _ := m["required"].([]any)
	for k := range inst {
		require.Contains(t, props, k, "example key %q is not a property of %s", k, id)
	}
	for _, r := range anyStrings(req) {
		require.Contains(t, inst, r, "example lacks required property %q of %s", r, id)
	}
	for k, v := range inst {
		if enum, ok := props[k].(map[string]any)["enum"].([]any); ok {
			require.Contains(t, anyStrings(enum), v, "%s=%v is not in the schema enum", k, v)
		}
	}
}

func TestReviewerExamplesAreValidReviewsOfTheSampleDiff(t *testing.T) {
	sample := readText(t, filepath.Join("..", "..", "skills", "reviewer", "examples", "sample.md"))
	for _, file := range exampleFiles(t, "reviewer") {
		t.Run(filepath.Base(file), func(t *testing.T) {
			var review finding.Review
			raw := decodeExample(t, file, &review)
			require.NoError(t, review.Validate(), "%s is not a valid Review@1", file)
			fitsTopLevel(t, "Review@1", raw)
			require.Contains(t, sample, string(review.HeadSHA), "%s must review the sample diff's head commit", file)
			for _, name := range review.FilesReviewed {
				require.Contains(t, sample, name, "%s reviewed %s, which the sample does not describe", file, name)
			}
			for _, f := range review.Findings {
				require.Contains(t, sample, f.Location.File, "%s files a finding on %s, which the sample does not describe", file, f.Location.File)
			}
		})
	}
}

func TestResolverExamplesAreValidVerdictsOnTheSampleCase(t *testing.T) {
	sample := readText(t, filepath.Join("..", "..", "skills", "resolver", "examples", "sample.md"))
	seen := map[deliberation.Decision]bool{}
	for _, file := range exampleFiles(t, "resolver") {
		t.Run(filepath.Base(file), func(t *testing.T) {
			var verdict deliberation.Verdict
			raw := decodeExample(t, file, &verdict)
			require.NoError(t, verdict.Validate(), "%s is not a valid Verdict@1", file)
			fitsTopLevel(t, "Verdict@1", raw)
			seen[verdict.Decision] = true
			require.Contains(t, sample, string(verdict.CaseID), "%s must settle the sample case", file)
			for _, c := range verdict.Citations {
				require.Contains(t, sample, c.Ref, "%s cites %s, which the sample does not describe", file, c.Ref)
			}
		})
	}
	for _, want := range []deliberation.Decision{deliberation.DecisionUphold, deliberation.DecisionDismiss, deliberation.DecisionNeedsHuman} {
		require.True(t, seen[want], "the resolver examples must include a %s verdict", want)
	}
}

func TestSkillsNameOnlyToolsTheServerOffersTheirRole(t *testing.T) {
	roles, later := specToolTable(t)
	for name := range documentedLater(t) {
		later[name] = true
	}
	implemented := implementedTools(t)
	for name := range later {
		require.NotNil(t, roles[name], "the later-tool list names %s, which TECH_SPEC 14.1 does not declare", name)
		require.False(t, implemented[name], "%s is implemented now: drop it from the later-tool list", name)
	}
	cases := []struct {
		skill string
		role  string
		must  []string
	}{
		{"reviewer", "reviewer", []string{"heimdall_get_task", "heimdall_ack", "heimdall_read_code", "heimdall_read_untrusted", "heimdall_submit_review"}},
		{"resolver", "resolver", []string{"heimdall_get_task", "heimdall_ack", "heimdall_read_code", "heimdall_read_untrusted", "heimdall_ask_humans", "heimdall_submit_verdict", "heimdall_report_progress"}},
	}
	for _, c := range cases {
		t.Run(c.skill, func(t *testing.T) {
			named := toolNames(skillBody(t, c.skill))
			require.NotEmpty(t, named, "skills/%s/SKILL.md names no tool", c.skill)
			for name := range named {
				allowed, declared := roles[name]
				require.True(t, declared, "skills/%s names %s, which the MCP tool table (TECH_SPEC 14.1) does not declare", c.skill, name)
				require.True(t, allowed[c.role] || allowed["all"], "skills/%s names %s, which the %s role does not have", c.skill, name, c.role)
				require.True(t, implemented[name] || later[name], "skills/%s names %s, which the MCP server neither implements nor documents as a later task", c.skill, name)
			}
			for _, name := range c.must {
				require.True(t, named[name], "skills/%s/SKILL.md must name %s", c.skill, name)
			}
		})
	}
}

func TestSkillsCarryNoEmDashAndNoPlaceholderWords(t *testing.T) {
	for _, skill := range []string{"reviewer", "resolver"} {
		body := skillBody(t, skill)
		require.NotContains(t, body, "\u2014", "skills/%s/SKILL.md must not use an em dash", skill)
		require.NotRegexp(t, placeholder, body, "skills/%s/SKILL.md must not contain TODO or TBD", skill)
	}
}

func TestSkillsCarryTheSameFrontMatterAsTheTestQualitySkill(t *testing.T) {
	for _, skill := range []string{"reviewer", "resolver"} {
		body := skillBody(t, skill)
		require.True(t, strings.HasPrefix(body, "---\nname: "+skill+"\n"), "skills/%s/SKILL.md must open with front matter naming the skill", skill)
		head, _, ok := strings.Cut(body, "\n---\n")
		require.True(t, ok, "skills/%s/SKILL.md front matter is not closed", skill)
		require.Contains(t, head, "description: ", "skills/%s/SKILL.md front matter needs a description", skill)
	}
}

// toolNames returns the tool names a skill names.
func toolNames(body string) map[string]bool {
	names := map[string]bool{}
	for _, name := range toolName.FindAllString(body, -1) {
		names[name] = true
	}
	return names
}

// specToolTable reads the tool table of TECH_SPEC 14.1 and returns, per tool, the roles that may call it, and the
// set of tools the table marks as later work. A parse that finds too few rows fails, so a changed table cannot make
// the check pass on nothing.
func specToolTable(t *testing.T) (roles map[string]map[string]bool, later map[string]bool) {
	t.Helper()
	body := readText(t, filepath.Join("..", "..", "docs", "TECH_SPEC.md"))
	roles, later = map[string]map[string]bool{}, map[string]bool{}
	section := false
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "### 14.1") {
			section = true
			continue
		}
		if section && strings.HasPrefix(line, "### ") {
			section = false
		}
		if !section || !strings.HasPrefix(line, "| `heimdall_") {
			continue
		}
		cells := strings.Split(line, "|")
		require.GreaterOrEqual(t, len(cells), 3, "malformed tool table row: %s", line)
		names := toolName.FindAllString(cells[1], -1)
		require.NotEmpty(t, names, "malformed tool table row: %s", line)
		allowed := roleSet(cells[2])
		for _, name := range names {
			roles[name] = allowed
			if strings.Contains(line, "(Later") {
				later[name] = true
			}
		}
	}
	require.GreaterOrEqual(t, len(roles), 10, "the tool table of TECH_SPEC 14.1 was not parsed")
	return roles, later
}

// roleSet returns the roles named in a tool table cell, with a parenthetical note removed.
func roleSet(cell string) map[string]bool {
	set := map[string]bool{}
	cell = regexp.MustCompile(`\([^)]*\)`).ReplaceAllString(cell, "")
	for _, role := range strings.Split(cell, ",") {
		if role = strings.ToLower(strings.TrimSpace(role)); role != "" {
			set[role] = true
		}
	}
	return set
}

// implementedTools reads the tool table the MCP server builds today.
func implementedTools(t *testing.T) map[string]bool {
	t.Helper()
	body := readText(t, filepath.Join("..", "..", "internal", "transport", "mcp", "tools.go"))
	set := map[string]bool{}
	for _, m := range regexp.MustCompile(`name: "(heimdall_[a-z][a-z_]*)"`).FindAllStringSubmatch(body, -1) {
		set[m[1]] = true
	}
	require.NotEmpty(t, set, "no tool names were found in internal/transport/mcp/tools.go")
	return set
}

// documentedLater reads the tools the MCP server's own doc.go says belong to later tasks. A skill may name one of
// them, because the spec declares it and the task that builds it is known; it may name nothing else that is missing.
func documentedLater(t *testing.T) map[string]bool {
	t.Helper()
	body := readText(t, filepath.Join("..", "..", "internal", "transport", "mcp", "doc.go"))
	m := regexp.MustCompile(`\(([a-z_, ]+)\) belongs to later tasks`).FindStringSubmatch(body)
	require.Len(t, m, 2, "internal/transport/mcp/doc.go no longer lists the tools that belong to later tasks")
	set := map[string]bool{}
	for _, name := range strings.Split(m[1], ",") {
		if name = strings.TrimSpace(name); name != "" {
			set["heimdall_"+name] = true
		}
	}
	require.NotEmpty(t, set)
	return set
}
