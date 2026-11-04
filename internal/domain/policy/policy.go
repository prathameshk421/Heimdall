package policy

import "github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"

// Policy is heimdall.yaml (TECH_SPEC section 16). It is read from the merge base, never the PR head
// (invariant 5). Every key of the spec example has a field, and decoding refuses keys that have none.
type Policy struct {
	Version      int                 `yaml:"version" json:"version"`
	Workflow     string              `yaml:"workflow" json:"workflow"`
	Approvers    map[string][]string `yaml:"approvers" json:"approvers"`
	PolicyOwners []string            `yaml:"policy_owners" json:"policy_owners"`
	People       map[string]Person   `yaml:"people" json:"people"`
	Domains      map[string]string   `yaml:"domains" json:"domains"`
	Reviewers    Reviewers           `yaml:"reviewers" json:"reviewers"`
	Slop         Slop                `yaml:"slop" json:"slop"`
	Tests        Tests               `yaml:"tests" json:"tests"`
	Blocking     Blocking            `yaml:"blocking" json:"blocking"`
	Consensus    Consensus           `yaml:"consensus" json:"consensus"`
	Deliberation Deliberation        `yaml:"deliberation" json:"deliberation"`
	Keys         Keys                `yaml:"keys" json:"keys"`
	Models       Models              `yaml:"models" json:"models"`
	Budgets      Budgets             `yaml:"budgets" json:"budgets"`
	Skills       Skills              `yaml:"skills" json:"skills"`
	Memory       Memory              `yaml:"memory" json:"memory"`
	Pack         Pack                `yaml:"pack" json:"pack"`
	Timeouts     Timeouts            `yaml:"timeouts" json:"timeouts"`
	CIRequired   []string            `yaml:"ci_required" json:"ci_required"`
	Chat         Chat                `yaml:"chat" json:"chat"`
	GitHub       GitHub              `yaml:"github" json:"github"`
	Tracker      Tracker             `yaml:"tracker" json:"tracker"`
	Agents       Agents              `yaml:"agents" json:"agents"`
}

// Person is one entry of people. GitHub is required only for authors and approvers (D158); Chat is the
// opaque Switch sender id; Agent is the reviewer agent name, if the person has one.
type Person struct {
	GitHub string   `yaml:"github,omitempty" json:"github,omitempty"`
	Chat   string   `yaml:"chat,omitempty" json:"chat,omitempty"`
	Agent  string   `yaml:"agent,omitempty" json:"agent,omitempty"`
	Teams  []string `yaml:"teams,omitempty" json:"teams,omitempty"`
}

// Reviewers sets how many reviewer agents are picked and which people or agents a path always needs.
type Reviewers struct {
	Count       int                 `yaml:"count" json:"count"`
	PickTimeout Duration            `yaml:"pick_timeout" json:"pick_timeout"`
	Required    map[string][]string `yaml:"required" json:"required"`
}

// Slop is the size and ticket rule of TECH_SPEC 12.3.
type Slop struct {
	MaxChangedLines int      `yaml:"max_changed_lines" json:"max_changed_lines"`
	RequireTicket   bool     `yaml:"require_ticket" json:"require_ticket"`
	Ignore          []string `yaml:"ignore" json:"ignore"`
}

// Tests is the test set rule: where the heimdall tests live, which imports they may use, which paths are
// sensitive, and the mutation score (TECH_SPEC 12.1, D131).
type Tests struct {
	Dir              string   `yaml:"dir" json:"dir"`
	AllowedImports   []string `yaml:"allowed_imports" json:"allowed_imports"`
	Sensitive        []string `yaml:"sensitive" json:"sensitive"`
	MinMutationScore float64  `yaml:"min_mutation_score" json:"min_mutation_score"`
	MutationBlocks   bool     `yaml:"mutation_blocks" json:"mutation_blocks"`
}

// Blocking is the lowest severity and evidence tier that can hold a merge (TECH_SPEC 11.1).
type Blocking struct {
	MinSeverity finding.Severity `yaml:"min_severity" json:"min_severity"`
	MinTier     finding.Tier     `yaml:"min_tier" json:"min_tier"`
}

// Consensus is the rule for reviewers who agree (TECH_SPEC 11.2b).
type Consensus struct {
	MinHighReporters int        `yaml:"min_high_reporters" json:"min_high_reporters"`
	BlockScore       float64    `yaml:"block_score" json:"block_score"`
	MinReporters     string     `yaml:"min_reporters" json:"min_reporters"`
	Confidence       Confidence `yaml:"confidence" json:"confidence"`
	Weight           Weight     `yaml:"weight" json:"weight"`
}

// Confidence maps each confidence level to its weight. The spec fixes the values, so Validate refuses others.
type Confidence struct {
	High   float64 `yaml:"high" json:"high"`
	Medium float64 `yaml:"medium" json:"medium"`
	Low    float64 `yaml:"low" json:"low"`
}

// Weight bounds a reviewer's calibration weight and says how many decided findings it takes to leave the start.
type Weight struct {
	Start      float64 `yaml:"start" json:"start"`
	Min        float64 `yaml:"min" json:"min"`
	Max        float64 `yaml:"max" json:"max"`
	MinDecided int     `yaml:"min_decided" json:"min_decided"`
}

// Deliberation limits the challenge rounds and names the domains no agent can settle (TECH_SPEC 11.4).
type Deliberation struct {
	MaxRounds        int      `yaml:"max_rounds" json:"max_rounds"`
	HumanOnlyDomains []string `yaml:"human_only_domains" json:"human_only_domains"`
}

// Keys names the OpenRouter key the deployment uses (D128).
type Keys struct {
	OpenRouter string `yaml:"openrouter" json:"openrouter"`
}

// Models names the model each role runs on. Chat models carry the provider prefix that OpenCode splits on (D143).
type Models struct {
	Provider   string `yaml:"provider" json:"provider"`
	TestAuthor string `yaml:"test_author" json:"test_author"`
	Reviewer   string `yaml:"reviewer" json:"reviewer"`
	Resolver   string `yaml:"resolver" json:"resolver"`
	Extraction string `yaml:"extraction" json:"extraction"`
	Embedding  string `yaml:"embedding" json:"embedding"`
}

// Budgets caps the tokens each role may spend on one change, and the whole change.
type Budgets struct {
	TestAuthor int `yaml:"test_author" json:"test_author"`
	Reviewer   int `yaml:"reviewer" json:"reviewer"`
	Resolver   int `yaml:"resolver" json:"resolver"`
	PerChange  int `yaml:"per_change" json:"per_change"`
}

// Skills pins the version of each role's skill, written as major.minor.patch.
type Skills struct {
	TestAuthor string `yaml:"test_author" json:"test_author"`
	Reviewer   string `yaml:"reviewer" json:"reviewer"`
	Resolver   string `yaml:"resolver" json:"resolver"`
}

// Memory says which rooms and chat keywords feed team memory (TECH_SPEC 9).
type Memory struct {
	Rooms        []string `yaml:"rooms" json:"rooms"`
	ChatKeywords []string `yaml:"chat_keywords" json:"chat_keywords"`
}

// Pack sets how old a context pack may be before it is refused.
type Pack struct {
	MaxAgeDays int `yaml:"max_age_days" json:"max_age_days"`
}

// Timeouts are the human and silence deadlines (TECH_SPEC 16). Silence never moves a change toward merge.
type Timeouts struct {
	Question      Duration `yaml:"question" json:"question"`
	RemindAfter   Duration `yaml:"remind_after" json:"remind_after"`
	EscalateAfter Duration `yaml:"escalate_after" json:"escalate_after"`
	RejectAfter   Duration `yaml:"reject_after" json:"reject_after"`
}

// Chat is where the change room lives and how much the bot says.
type Chat struct {
	Bridge          string `yaml:"bridge" json:"bridge"`
	Verbosity       string `yaml:"verbosity" json:"verbosity"`
	DecisionsInRoom bool   `yaml:"decisions_in_room" json:"decisions_in_room"`
}

// GitHub is how Heimdall learns about pull requests (D64).
type GitHub struct {
	Mode     string   `yaml:"mode" json:"mode"`
	Interval Duration `yaml:"interval" json:"interval"`
}

// Tracker is where tickets come from (D50). Kind is jira, or file in local mode.
type Tracker struct {
	Kind     string   `yaml:"kind" json:"kind"`
	Mode     string   `yaml:"mode" json:"mode"`
	Interval Duration `yaml:"interval" json:"interval"`
}

// Agents names the runtime the VM agents run on (D72, D145).
type Agents struct {
	Runtime string `yaml:"runtime" json:"runtime"`
}
