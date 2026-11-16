package main

import (
	"fmt"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
)

// signoffPayload is the tests.approved a human approver writes for a change that waits for test sign-off. It is bound
// to the tests tree of the verified run, so an approval never lands on tests nobody reviewed (A4, D33). A change that
// is not waiting for sign-off refuses it.
func signoffPayload(v change.View, approver string) (change.TestsApproved, error) {
	if err := change.CanApproveTests(v); err != nil {
		return change.TestsApproved{}, fmt.Errorf("nothing is waiting for test sign-off on this change: %w", err)
	}
	return change.TestsApproved{Approver: change.ActorID(approver), TreeHash: v.Tests.Verified.TreeHash}, nil
}
