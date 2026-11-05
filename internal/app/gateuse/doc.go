// Package gateuse evaluates the merge rule for one change and publishes the result as a GitHub check
// (TECH_SPEC 12.4, tasks T47 and K2). RepoSource reads external facts like repository policies, diffs, and test
// trees through ports.PRReader and ports.CodeReader.
package gateuse
