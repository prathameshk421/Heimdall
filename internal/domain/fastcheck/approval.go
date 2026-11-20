package fastcheck

import (
	"slices"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/gate"
)

// checkApproval reuses the gate's approval clause: an approval on head, not from the author, by an approver of the
// changed domains. A head, an author or a review list that is not known is unknown, not a failure of the person who
// approved.
func checkApproval(f Facts) Clause {
	switch {
	case !f.ApprovalsKnown:
		return unknown(ClauseApproval, "the reviews of this pull request are not known")
	case f.HeadSHA == "":
		return unknown(ClauseApproval, "the head commit is not known")
	case f.AuthorLogin == "":
		return unknown(ClauseApproval, "the pull request author is not known")
	}
	reason := gate.ApprovalReason(gate.Facts{
		AuthorLogin:                f.AuthorLogin,
		HeadSHA:                    f.HeadSHA,
		Approvals:                  f.Approvals,
		ApproversForChangedDomains: f.ApproversForChangedDomains,
	})
	if reason != "" {
		return unmet(ClauseApproval, reason)
	}
	return met(ClauseApproval, "an approver of the changed domains approved head")
}

// ownerApproval says whether a policy owner approved on head. known is false when the reviews, the head or the author
// are not known, because then nobody can say.
func ownerApproval(f Facts) (approved, known bool) {
	if !f.ApprovalsKnown || f.HeadSHA == "" || f.AuthorLogin == "" {
		return false, false
	}
	for _, a := range f.Approvals {
		if a.Approved && a.Commit == f.HeadSHA && a.Login != f.AuthorLogin && slices.Contains(f.PolicyOwners, a.Login) {
			return true, true
		}
	}
	return false, true
}

// exempt decides a clause that has a violation and that a policy owner can clear. The violation is the text that says
// what was found. An owner on head clears it, it stays unmet when no owner approved, and it is unknown when nobody can
// say whether one did.
func exempt(id ClauseID, f Facts, violation string) Clause {
	approved, known := ownerApproval(f)
	switch {
	case approved:
		return met(id, violation+", and a policy owner approved on head")
	case !known:
		return unknown(id, violation+", and it is not known whether a policy owner approved on head")
	}
	return unmet(id, violation+", and no policy owner approved on head")
}
