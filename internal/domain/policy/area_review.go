package policy

import "github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/finding"

// ReviewArea holds the review keys of the policy: how reviewers are picked, the slop rule and deliberation. Blocking
// and Consensus are review keys too; they are named fields of Policy (see Policy).
type ReviewArea struct {
	// Seats are the reviewer agents and the lenses each covers (AGENT_INTERACTION_ARCH F3, REVIEW_ARCH 5.3), keyed by
	// agent name. A person lends a seat their name and cards through people.<p>.seat.
	Seats        map[string]Seat `yaml:"seats,omitempty" json:"seats,omitempty"`
	Reviewers    Reviewers       `yaml:"reviewers" json:"reviewers"`
	Slop         Slop            `yaml:"slop" json:"slop"`
	Deliberation Deliberation    `yaml:"deliberation" json:"deliberation"`
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

// Seat is one reviewer agent: the Switch role it holds (so @security reaches it) and its lenses.
type Seat struct {
	Role   string   `yaml:"role" json:"role"`
	Lenses []string `yaml:"lenses" json:"lenses"`
}
