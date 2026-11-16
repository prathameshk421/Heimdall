package policy

// ChecksArea holds the checks keys of the policy: the CI checks a merge needs.
type ChecksArea struct {
	CIRequired []string `yaml:"ci_required" json:"ci_required"`
}
