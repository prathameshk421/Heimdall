package external_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prathameshk421/flip_the_switch_hackathon/internal/domain/external"
)

func TestRecognise(t *testing.T) {
	base := inline(1, "a.go", 3)
	with := func(f func(c *external.Comment)) external.Comment { c := base; f(&c); return c }
	cases := []struct {
		name string
		c    external.Comment
		pr   external.PR
		want external.IgnoreReason
	}{
		{"known bot with its marker is read", base, newPR(), ""},
		{"login case and [bot] suffix do not matter", with(func(c *external.Comment) { c.UserLogin = "CodeRabbitAI" }), newPR(), ""},
		{"unknown bot is ignored", with(func(c *external.Comment) { c.UserLogin = "dependabot[bot]" }), newPR(), external.IgnoreUnknownAuthor},
		{"a human is ignored", with(func(c *external.Comment) { c.UserLogin = "alice" }), newPR(), external.IgnoreUnknownAuthor},
		{"the right login on a user account is ignored", with(func(c *external.Comment) { c.UserType = "User" }), newPR(), external.IgnoreNotBot},
		{"a tool missing from sources is ignored", base, external.PR{Head: head, Sources: []string{"greptile-apps[bot]"}}, external.IgnoreNotInSources},
		{"no sources means nobody counts", base, external.PR{Head: head}, external.IgnoreNotInSources},
		{"a reply in a thread is ignored", with(func(c *external.Comment) { c.InReplyTo = 99 }), newPR(), external.IgnoreReply},
		{"the right login with no marker of its tool is ignored", with(func(c *external.Comment) { c.Body = "Thanks, I will look at it." }), newPR(), external.IgnoreNoMarker},
		{"a body that is only a badge has no claim", with(func(c *external.Comment) {
			c.Body = "_⚠️ Potential issue_ | _🟡 Minor_\n\n![badge](https://x.test/a.svg)"
		}), newPR(), external.IgnoreNoClaim},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.pr.Ingest(tc.c)
			if tc.want == "" {
				require.Equal(t, external.OutcomeCandidate, got.Outcome, "%+v", got)
				return
			}
			require.Equal(t, external.OutcomeIgnored, got.Outcome)
			require.Equal(t, tc.want, got.Ignore)
		})
	}
}

// TestEachToolByLoginAndMarker: the four tools and their logins. A login is never enough for the tools that mark their bodies.
func TestEachToolByLoginAndMarker(t *testing.T) {
	cases := []struct {
		login, body string
		want        external.Tool
	}{
		{"coderabbitai[bot]", "_⚠️ Potential issue_ | _🟠 Major_\n\n**Bad thing happens here.**", external.ToolCodeRabbit},
		{"copilot-pull-request-reviewer[bot]", "Bad thing happens on this line of the file.", external.ToolCopilot},
		{"Copilot", "Bad thing happens on this line of the file.", external.ToolCopilot},
		{"cursor[bot]", "### Bad thing\n\n**Low Severity**\n\n<!-- DESCRIPTION START -->\nBad thing happens.\n<!-- DESCRIPTION END -->", external.ToolBugbot},
		{"greptile-apps[bot]", "**logic:** bad thing happens on this line of the file.", external.ToolGreptile},
	}
	for _, tc := range cases {
		c := inline(1, "a.go", 3)
		c.UserLogin, c.Body = tc.login, tc.body
		pr := newPR()
		pr.Sources = append(pr.Sources, "Copilot")
		got := pr.Ingest(c)
		require.Equal(t, external.OutcomeCandidate, got.Outcome, "%s: %+v", tc.login, got)
		require.Equal(t, tc.want, got.Tool)
	}
	// Bugbot's login with a CodeRabbit body is not Bugbot's comment.
	c := inline(1, "a.go", 3)
	c.UserLogin = "cursor[bot]"
	require.Equal(t, external.IgnoreNoMarker, newPR().Ingest(c).Ignore)
}

func TestClaimCleaning(t *testing.T) {
	long := strings.Repeat("word ", 400)
	cases := []struct{ name, body, want string }{
		{"html comment and unterminated details are stripped",
			"**Check the error.**\n\nIt is dropped.<!-- hidden -->\n<details><summary>x</summary>never closed",
			"Check the error. It is dropped."},
		{"nested details are stripped as one block",
			"**Check the error.**\n<details><details>a</details>b</details>\nIt is dropped.",
			"Check the error. It is dropped."},
		{"an unterminated suggestion block is stripped to the end",
			"**Check the error.**\n```suggestion\nx := 1\nstill code", "Check the error."},
		{"inline html tags are removed", "**Check the error.**<br>It is <b>dropped</b> here.", "Check the error. It is dropped here."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := inline(1, "a.go", 3)
			c.Body = "_⚠️ Potential issue_ | _🟡 Minor_\n\n" + tc.body
			got := newPR().Ingest(c)
			require.Equal(t, external.OutcomeCandidate, got.Outcome, "%+v", got)
			require.Equal(t, tc.want, got.Candidate.Claim)
		})
	}
	t.Run("a long claim is cut to 600 characters", func(t *testing.T) {
		c := inline(1, "a.go", 3)
		c.Body = "_⚠️ Potential issue_ | _🟡 Minor_\n\n" + long
		got := newPR().Ingest(c)
		require.Equal(t, external.OutcomeCandidate, got.Outcome)
		require.Len(t, []rune(got.Candidate.Claim), 600)
		require.True(t, strings.HasSuffix(got.Candidate.Claim, "…"))
	})
}
