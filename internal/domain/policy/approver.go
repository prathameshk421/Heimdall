package policy

import "slices"

// ApproverOf reports whether the person may approve changes to file: the person is in the approver group of a domain
// that matches the path, or in the default group when no domain matches. This is the same rule the gate uses for a
// GitHub approval. A policy owner gets nothing extra here, because owning the policy file is not owning this code.
func (p Policy) ApproverOf(person, file string) bool {
	matched := false
	for pattern, group := range p.Domains {
		if !MatchGlob(pattern, file) {
			continue
		}
		matched = true
		if slices.Contains(p.Approvers[group], person) {
			return true
		}
	}
	return !matched && slices.Contains(p.Approvers["default"], person)
}
