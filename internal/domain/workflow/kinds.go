package workflow

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

// Kind is one kind of agent work the server waits for (AGENT_INTERACTION_ARCH F2, HARNESS_DESIGN B6). It is the one
// table the board, the wake line, get_task, the tool list and the skill read, so a role is described in one place and
// nothing else switches on it.
type Kind struct {
	Name   string   // "review"
	Holder Role     // who may hold it
	Inputs []string // input names, rendered in this order by get_task
	Tools  []string // tools offered while a task of this kind is active, besides the shared ones
	Output string   // the schema its submission follows, such as Review@2
	Skill  string   // the file under skills/ that holds its instructions
	// Timeout is how long an agent may hold a task of this kind before it is timed out.
	Timeout time.Duration
	Work    string // what the agent will do, in a few plain words, for the wake line
	Label   string // how the board names the holder
}

// SharedTools are offered for every kind.
func SharedTools() []string { return []string{"get_task", "report_progress", "propose_command"} }

// MaxSkillWords is the length limit of a skill file.
const MaxSkillWords = 400

// Kinds is the table, one row per kind.
func Kinds() []Kind {
	return []Kind{
		{Name: "tests", Holder: RoleTestAuthor, Output: "TestSubmission@1", Label: "the Test Agent", Skill: "tests.md", Timeout: 2 * time.Hour,
			Work:   "write failing tests for the acceptance criteria",
			Inputs: []string{"ticket", "profile", "repo_map", "rejection", "answers"},
			Tools:  []string{"read_code", "search_code", "ask_humans", "submit_tests"}},
		{Name: "review", Holder: RoleReviewer, Output: "Review@2", Label: "a Code Reviewer", Skill: "review.md", Timeout: 30 * time.Minute,
			Work:   "review the pull request",
			Inputs: []string{"pr", "files", "diff", "criteria", "obligations", "leads", "prior_findings", "team_memory", "repo_map"},
			Tools:  []string{"read_code", "search_code", "find_symbol", "run_check", "ask_knowledge", "read_card", "stage_knowledge", "submit_review"}},
		{Name: "verdict", Holder: RoleResolver, Output: "Verdict@1", Label: "the Resolver", Skill: "verdict.md", Timeout: 30 * time.Minute,
			Work:   "settle the challenged findings",
			Inputs: []string{"case", "prior_decisions", "diff"},
			Tools:  []string{"read_code", "run_check", "read_card", "ask_knowledge", "submit_verdict"}},
		{Name: "profile", Holder: RoleProfiler, Output: "Profile@1", Label: "the Profiler", Skill: "profile.md", Timeout: time.Hour,
			Work:   "propose a sandbox profile for the repository",
			Inputs: []string{"repo_map", "stack_hints", "previous_trial"},
			Tools:  []string{"read_code", "search_code", "propose_profile"}},
	}
}

// KindFor returns the kind a role holds today. Each role holds one kind until the later kinds land.
func KindFor(r Role) (Kind, bool) {
	for _, k := range Kinds() {
		if k.Holder == r {
			return k, true
		}
	}
	return Kind{}, false
}

// KindNamed returns the kind with the name.
func KindNamed(name string) (Kind, bool) {
	for _, k := range Kinds() {
		if k.Name == name {
			return k, true
		}
	}
	return Kind{}, false
}

// Registry is what the table is checked against: the tools the server registers, the input renderers it has, the
// schema names it knows and the text of each skill file.
type Registry struct {
	Tools     []string
	Renderers []string
	Schemas   []string
	Skills    map[string]string
}

// CheckKinds reports every way the table and the server disagree: a kind names a tool that is not registered, an input
// without a renderer, an output schema nobody registered, a skill file that is missing or too long, or a skill that
// names a tool the kind does not hold. A server that fails this check does not start.
func CheckKinds(r Registry) []error {
	var problems []error
	all := SharedTools()
	for _, k := range Kinds() {
		all = append(all, k.Tools...)
	}
	for _, k := range Kinds() {
		for _, t := range k.Tools {
			if !slices.Contains(r.Tools, t) {
				problems = append(problems, fmt.Errorf("kind %s: tool %q is not registered", k.Name, t))
			}
		}
		for _, in := range k.Inputs {
			if !slices.Contains(r.Renderers, in) {
				problems = append(problems, fmt.Errorf("kind %s: input %q has no renderer", k.Name, in))
			}
		}
		if !slices.Contains(r.Schemas, k.Output) {
			problems = append(problems, fmt.Errorf("kind %s: output schema %q is not registered", k.Name, k.Output))
		}
		problems = append(problems, checkSkill(k, all, r.Skills)...)
	}
	return problems
}

func checkSkill(k Kind, allTools []string, skills map[string]string) []error {
	text, ok := skills[k.Skill]
	if !ok {
		return []error{fmt.Errorf("kind %s: skill file %q does not exist", k.Name, k.Skill)}
	}
	var problems []error
	if n := len(strings.Fields(text)); n > MaxSkillWords {
		problems = append(problems, fmt.Errorf("kind %s: skill %s has %d words, the limit is %d", k.Name, k.Skill, n, MaxSkillWords))
	}
	held := append(SharedTools(), k.Tools...)
	for _, t := range allTools {
		if !slices.Contains(held, t) && strings.Contains(text, "`"+t+"`") {
			problems = append(problems, fmt.Errorf("kind %s: skill %s names tool %q, which the kind does not hold", k.Name, k.Skill, t))
		}
	}
	return problems
}
