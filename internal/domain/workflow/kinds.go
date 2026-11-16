package workflow

// Kind is one kind of agent work the server waits for (AGENT_INTERACTION_ARCH F2). The bundle, the wake line and the
// board read this table, so a role is described in one place and nowhere else switches on it. A kind that a later
// module builds (test_plan, survivors, critique, refute) gets its row when its work lands.
type Kind struct {
	Name   string // "review"
	Holder Role   // who may hold it
	Output string // the schema its submission follows
	Work   string // what the agent will do, in a few plain words, for the wake line
	Label  string // how the board names the holder
	Rules  string // the one-line rule at the top of the brief
	Budget int    // the context budget of its bundle in tokens; 0 means the bundle is not capped
}

// Kinds is the table, one row per kind.
func Kinds() []Kind {
	return []Kind{
		{Name: "tests", Holder: RoleTestAuthor, Output: "TestSubmission@1", Label: "the Test Agent", Budget: 40000,
			Work:  "write failing tests for the acceptance criteria",
			Rules: "Write tests for every acceptance criterion; use only the base source and declared seam."},
		{Name: "review", Holder: RoleReviewer, Output: "Review@1", Label: "a Code Reviewer", Budget: 60000,
			Work:  "review the pull request",
			Rules: "Review the change from the owner's perspective; file only evidence-backed findings."},
		{Name: "verdict", Holder: RoleResolver, Output: "Verdict@1", Label: "the Resolver", Budget: 30000,
			Work:  "settle the challenged findings",
			Rules: "Recommend a verdict from the case evidence and cite prior decisions with validity times."},
		{Name: "profile", Holder: RoleProfiler, Output: "Profile@1", Label: "the Profiler",
			Work: "propose a sandbox profile for the repository"},
	}
}

// KindFor returns the kind a role holds today. Each role holds one kind until the later kinds land.
func KindFor(r Role) (Kind, bool) {
	for _, k := range Kinds() {
		if k.Holder == r {
			return k, true
		}
	}
	return Kind{}, false
}
