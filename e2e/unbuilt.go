package e2e

import "context"

// The drivers below belong to tasks that have not landed. Each returns a NotBuiltError naming the task, so a step
// that needs one is blocked on that task rather than failing. The task that builds a driver replaces it.

func unbuilt(task, what string) error { return &NotBuiltError{Task: task, What: what} }

type unbuiltTracker struct{}

func (unbuiltTracker) CreateTicket(context.Context, Ticket) (string, error) {
	return "", unbuilt("T36", "the Jira driver")
}
func (unbuiltTracker) Move(context.Context, string, string) error {
	return unbuilt("T36", "the Jira driver")
}
func (unbuiltTracker) Edit(context.Context, string, Ticket) error {
	return unbuilt("T36", "the Jira driver")
}

type unbuiltGitHub struct{}

func (unbuiltGitHub) Push(context.Context, string, string, string, Change) (string, error) {
	return "", unbuilt("T31", "the GitHub driver")
}
func (unbuiltGitHub) SetHead(context.Context, PR, string, string) (PR, error) {
	return PR{}, unbuilt("T31", "the GitHub driver")
}
func (unbuiltGitHub) Comments(context.Context, PR) ([]PRComment, error) {
	return nil, unbuilt("T31", "the GitHub driver")
}
func (unbuiltGitHub) ChangedFiles(context.Context, string, string, string) ([]string, error) {
	return nil, unbuilt("T31", "the GitHub driver")
}
func (unbuiltGitHub) ReadFile(context.Context, string, string, string) (string, error) {
	return "", unbuilt("T31", "the GitHub driver")
}
func (unbuiltGitHub) Mergeable(context.Context, PR) (bool, error) {
	return false, unbuilt("T31", "the GitHub driver")
}
func (unbuiltGitHub) OpenPR(context.Context, string, string, string, string, string, string) (PR, error) {
	return PR{}, unbuilt("T31", "the GitHub driver")
}
func (unbuiltGitHub) Comment(context.Context, PR, string, string) (int64, error) {
	return 0, unbuilt("T31", "the GitHub driver")
}
func (unbuiltGitHub) EditComment(context.Context, PR, string, int64, string) error {
	return unbuilt("T31", "the GitHub driver")
}
func (unbuiltGitHub) Review(context.Context, PR, string, string, string) error {
	return unbuilt("T31", "the GitHub driver")
}
func (unbuiltGitHub) Merge(context.Context, PR, string) error {
	return unbuilt("T31", "the GitHub driver")
}
func (unbuiltGitHub) Close(context.Context, PR) error { return unbuilt("T31", "the GitHub driver") }
func (unbuiltGitHub) Branches(context.Context, string) ([]string, error) {
	return nil, unbuilt("T31", "the GitHub driver")
}
func (unbuiltGitHub) FileLines(context.Context, string, string, string) (int, error) {
	return 0, unbuilt("T31", "the GitHub driver")
}
func (unbuiltGitHub) Commits(context.Context, string, string) ([]Commit, error) {
	return nil, unbuilt("T31", "the GitHub driver")
}

type unbuiltChat struct{}

func (unbuiltChat) Say(context.Context, string, string, string) error {
	return unbuilt("T26", "the chat driver")
}
func (unbuiltChat) Transcript(context.Context, string) ([]Message, error) {
	return nil, unbuilt("T26", "the chat driver")
}
func (unbuiltChat) Rooms(context.Context) ([]string, error) {
	return nil, unbuilt("T26", "the chat driver")
}
func (unbuiltChat) Members(context.Context, string) ([]string, error) {
	return nil, unbuilt("T26", "the chat driver")
}

type unbuiltAgents struct{}

func (unbuiltAgents) Stop(context.Context, string) error {
	return unbuilt("T27", "agent start and stop")
}
func (unbuiltAgents) Start(context.Context, string) error {
	return unbuilt("T27", "agent start and stop")
}
func (unbuiltAgents) Tasks(context.Context) (map[string]int, error) {
	return nil, unbuilt("T27", "the agent task log")
}

type unbuiltScripted struct{}

func (unbuiltScripted) Script(context.Context, string, string) error {
	return unbuilt("T38", "the scripted agent")
}

type unbuiltFaults struct{}

func (unbuiltFaults) Stop(context.Context, Component) error {
	return unbuilt("T74", "the fault injector")
}
func (unbuiltFaults) Start(context.Context, Component) error {
	return unbuilt("T74", "the fault injector")
}

type unbuiltRunner struct{}

func (unbuiltRunner) RunOnBase(context.Context, string, string, string) ([]TestRun, error) {
	return nil, unbuilt("T29", "the test runner on the base branch")
}

type unbuiltCleanRoom struct{}

func (unbuiltCleanRoom) Exec(context.Context, ...string) (CLIResult, error) {
	return CLIResult{}, unbuilt("T68", "the clean-container world")
}
func (unbuiltCleanRoom) WriteFile(context.Context, string, string) error {
	return unbuilt("T68", "the clean-container world")
}
func (unbuiltCleanRoom) Dashboard(context.Context) (string, error) {
	return "", unbuilt("T68", "the clean-container world")
}
