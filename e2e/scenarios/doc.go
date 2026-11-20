// Package scenarios holds the forty steps P1.1 to P9.4 of the prototype board (MVP_SCENARIOS section 3).
//
// Each step drives the product only through the e2e.World, as a person would, and asserts only what a person sees.
// Steps read the repository, ticket, people and planted issues from the e2e.Fixture (testdata/fixture-pay.yaml), so
// a step never names them itself. Steps of one scenario share values (the ticket key, the room, the PR) through Env.Vars.
//
// Where the spec leaves the product's wording open, the step fixes a contract the product is written to match;
// each is stated in the comment of the step that relies on it.
package scenarios
