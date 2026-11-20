package policy

// Policy is heimdall.yaml (TECH_SPEC section 16). It is read from the merge base, never the PR head
// (invariant 5). Every key of the spec example has a field, and decoding refuses keys that have none. The core keys
// are fields here; each other area embeds one struct (area_<area>.go) whose keys sit at the top level of the file.
type Policy struct {
	Version      int                 `yaml:"version" json:"version"`
	Workflow     string              `yaml:"workflow" json:"workflow"`
	Approvers    map[string][]string `yaml:"approvers" json:"approvers"`
	PolicyOwners []string            `yaml:"policy_owners" json:"policy_owners"`
	People       map[string]Person   `yaml:"people" json:"people"`
	Domains      map[string]string   `yaml:"domains" json:"domains"`
	ReviewArea   `yaml:",inline"`
	// Tests, Blocking and Consensus belong to the tests and review areas (area_tests.go, area_review.go). They stay
	// named fields here because callers build a Policy with them by name, which a promoted field does not allow.
	Tests      Tests     `yaml:"tests" json:"tests"`
	Blocking   Blocking  `yaml:"blocking" json:"blocking"`
	Consensus  Consensus `yaml:"consensus" json:"consensus"`
	Keys       Keys      `yaml:"keys" json:"keys"`
	Models     Models    `yaml:"models" json:"models"`
	Budgets    Budgets   `yaml:"budgets" json:"budgets"`
	Skills     Skills    `yaml:"skills" json:"skills"`
	Pack       Pack      `yaml:"pack" json:"pack"`
	TalkArea   `yaml:",inline"`
	ChecksArea `yaml:",inline"`
	ActionArea `yaml:",inline"`
	GitHub     GitHub  `yaml:"github" json:"github"`
	Tracker    Tracker `yaml:"tracker" json:"tracker"`
	Agents     Agents  `yaml:"agents" json:"agents"`
}

// Person is one entry of people. GitHub is required only for authors and approvers (D158); Chat is the
// opaque Switch sender id; Seat is the reviewer seat (an agent name) the person lends their name and cards to, if any
// (AGENT_INTERACTION_ARCH F3).
type Person struct {
	GitHub string   `yaml:"github,omitempty" json:"github,omitempty"`
	Chat   string   `yaml:"chat,omitempty" json:"chat,omitempty"`
	Seat   string   `yaml:"seat,omitempty" json:"seat,omitempty"`
	Teams  []string `yaml:"teams,omitempty" json:"teams,omitempty"`
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

// Pack sets how old a context pack may be before it is refused.
type Pack struct {
	MaxAgeDays int `yaml:"max_age_days" json:"max_age_days"`
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
