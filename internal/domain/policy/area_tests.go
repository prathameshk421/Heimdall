package policy

// The tests area of the policy is the tests key. It is a named field of Policy (see Policy), so this file holds its
// type only.

// Tests is the test set rule: where the heimdall tests live, which imports they may use, which paths are
// sensitive, and the mutation score (TECH_SPEC 12.1, D131).
type Tests struct {
	Dir              string   `yaml:"dir" json:"dir"`
	AllowedImports   []string `yaml:"allowed_imports" json:"allowed_imports"`
	Sensitive        []string `yaml:"sensitive" json:"sensitive"`
	MinMutationScore float64  `yaml:"min_mutation_score" json:"min_mutation_score"`
	MutationBlocks   bool     `yaml:"mutation_blocks" json:"mutation_blocks"`
}
