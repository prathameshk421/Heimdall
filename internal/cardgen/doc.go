// Package cardgen builds the prompt a developer's own coding agent follows to write that developer's personal context
// cards. Heimdall detects the repository, the git identity, the docs and the agent's past sessions, then hands this
// prompt to the agent the developer already uses; the agent does the distilling with the developer's own model and
// access, so no raw transcript or source leaves the laptop. The CLI then checks, redacts, shows and uploads the files.
//
// The prompt is the product here. It asks for what a generic reviewer cannot know: the rules this developer enforces,
// the incidents and corrections behind them, and how they weigh trade-offs, each with evidence, the paths it applies
// to and a consequence. Cards that a linter or a README already covers are not wanted.
//
// The package is pure: it formats text and does no I/O. Repository and session discovery live in cmd/heimdall.
package cardgen
