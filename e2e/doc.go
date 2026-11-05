// Package e2e is the user simulator and the prototype board (T98, MVP_SCENARIOS sections 2 to 6).
//
// A scenario is a script that drives the product only from the outside, the way people do, through a
// World: a tracker user, a GitHub user, a chat user, an observer of what those show, and the command line.
// It asserts only what a person would see. It never reads the database, calls an internal function or
// asserts on an event name. If the product is right, a person could see it.
//
// The board runs every step of every scenario and prints one cell per step:
//
//	pass         the step ran and every expectation and quality check held
//	red          the step ran and something was wrong; the reason is "expected X, saw Y"
//	blocked(x)   the step cannot run because prerequisite x is missing; blocked is never green
//	not reached  the step ran after an earlier step it depends on failed
//
// A prerequisite is a named fact that is really probed (the server answers, the port-forward is up, the fixture
// repository has its branches) or a driver that a later task builds (an error that carries the task id).
// The library is untagged so its logic is tested in `make fast`; the live per-task checks (TestTask_T##)
// carry the e2e build tag and live beside it.
package e2e
