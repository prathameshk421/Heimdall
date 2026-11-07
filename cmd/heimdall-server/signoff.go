package main

import (
	"errors"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"
)

// signoffPayload is the tests.approved a human approver writes for a change that waits for test sign-off. It is bound
// to the tests tree of the latest verified test run, so an approval never lands on tests nobody reviewed (A4, D33).
// A change that is not waiting for sign-off refuses it.
func signoffPayload(state change.State, events []change.Event, approver string) (change.TestsApproved, error) {
	if state != change.StateAwaitingTestSignoff {
		return change.TestsApproved{}, errors.New("nothing is waiting for test sign-off on this change")
	}
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Type != change.EventTestsVerified {
			continue
		}
		p, err := change.Decode(events[i])
		if err != nil {
			return change.TestsApproved{}, err
		}
		tv, ok := p.(change.TestsVerified)
		if !ok {
			return change.TestsApproved{}, errors.New("the latest test run is not a verified run")
		}
		return change.TestsApproved{Approver: change.ActorID(approver), TreeHash: tv.TreeHash}, nil
	}
	return change.TestsApproved{}, errors.New("nothing is waiting for test sign-off on this change")
}
