package people

import "github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/change"

// requirement is one role an action accepts. A rule lists the requirements; the sender needs any one of them.
type requirement int

const (
	needMember requirement = iota + 1
	needAuthor
	needApprover
	needDomainApprover
	needFilerOrApprover
)

// rule says who may take an action and what must hold on the change.
type rule struct {
	needs     []requirement
	notAuthor bool
	bindHead  bool
}

// rules is the authorisation table (TECH_SPEC 11.5 and 12.4). It is a function, not a variable, so no package
// state can be changed by a caller.
func rules() map[Action]rule {
	return map[Action]rule{
		change.DecisionReview:        {needs: []requirement{needAuthor}},
		change.DecisionUphold:        {needs: []requirement{needApprover}},
		change.DecisionDismiss:       {needs: []requirement{needFilerOrApprover}},
		change.DecisionSpecChange:    {needs: []requirement{needApprover}},
		change.DecisionOverride:      {needs: []requirement{needApprover}},
		change.DecisionWaiveReviewer: {needs: []requirement{needApprover}},
		change.DecisionReject:        {needs: []requirement{needApprover}},
		change.DecisionApproveTests:  {needs: []requirement{needDomainApprover}, notAuthor: true},
		change.DecisionIncident:      {needs: []requirement{needMember}},
		change.DecisionStart:         {needs: []requirement{needMember}},
		change.DecisionChallenge:     {needs: []requirement{needAuthor}},
		change.DecisionRemember:      {needs: []requirement{needMember}},
		change.DecisionForget:        {needs: []requirement{needApprover}},
		change.DecisionResume:        {needs: []requirement{needApprover}},
		ActionApprove:                {needs: []requirement{needApprover}, notAuthor: true, bindHead: true},
	}
}
