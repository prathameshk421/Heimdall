// Package gateuse evaluates the merge rule for one change and publishes the result as a GitHub check
// (TECH_SPEC 12.4, tasks T47 and K2). RepoSource reads external facts like repository policies, diffs, and test
// trees through ports.PRReader and ports.CodeReader.
//
// A finding's severity comes from the finding event it was filed with (A1). The blocking set applies the policy's
// blocking.min_severity and tier at the merge base; with no policy known, every open E1 to E3 finding blocks.
package gateuse
