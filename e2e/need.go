package e2e

import (
	"errors"
	"fmt"
)

// Need names a prerequisite of a step. The names appear on the board as blocked(name).
type Need string

// The prerequisites a step can declare.
const (
	NeedServer      Need = "server"       // heimdall-server answers its health check
	NeedForward     Need = "forward"      // the SSH port-forward to the VM's Switch and Mattermost is up
	NeedFixtureRepo Need = "fixture-repo" // the Story's repository has its base branch and PR branches
	NeedTracker     Need = "tracker"      // a Jira driver that can create and move tickets
	NeedGitHubUser  Need = "github-user"  // a GitHub driver that can push, open PRs and review
	NeedChatUser    Need = "chat-user"    // a chat driver that can speak as a person and read a room
	NeedScripted    Need = "scripted"     // the scripted agent (T38) is registered
	NeedLive        Need = "live-agents"  // real OpenCode agents, and a budgeted key
	NeedMemory      Need = "memory"       // Graphiti is up and seeded
	NeedContainer   Need = "container"    // a clean container with its own Docker daemon (T68)
	NeedManual      Need = "manual"       // a person must do it
	NeedCLI         Need = "cli"          // the heimdall command-line binary builds
)

// Availability is the answer of a probe: whether the prerequisite holds and, if not, the plain reason.
type Availability struct {
	OK  bool
	Why string
}

// NotBuiltError says a simulator driver belongs to a task that has not landed. A step that meets it is
// blocked, not red: the product is not wrong, the simulator cannot act yet.
type NotBuiltError struct {
	Task string
	What string
}

func (e *NotBuiltError) Error() string {
	return fmt.Sprintf("%s is not built yet (%s)", e.What, e.Task)
}

// IsNotBuilt reports whether err says a driver is not built, and for which task.
func IsNotBuilt(err error) (task string, ok bool) {
	var nb *NotBuiltError
	if errors.As(err, &nb) {
		return nb.Task, true
	}
	return "", false
}

// Red is a failed expectation: what the person should have seen and what they saw.
type Red struct {
	Expected string
	Saw      string
}

func (r *Red) Error() string { return fmt.Sprintf("expected %s, saw %s", r.Expected, r.Saw) }
