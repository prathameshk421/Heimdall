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

// TestEachToolByLoginAndMarker: the tools and their logins. A login is never enough for the tools that mark their
// bodies. The bodies are the real formats, quoted from the public data where the format is short.
func TestEachToolByLoginAndMarker(t *testing.T) {
	const (
		copilotProse = "Bad thing happens on this line of the file."
		codexBody    = "**<sub><sub>![P2 Badge](https://img.shields.io/badge/P2-yellow?style=flat)</sub></sub>  Title**\n\n" +
			"The body of the finding is here.\n\nUseful? React with 👍 / 👎."
		devinBody = "<!-- devin-review-comment {\"id\": \"BUG_1\", \"file_path\": \"a.go\", \"start_line\": 3, \"end_line\": 3} -->\n\n" +
			"🟠 **A thing breaks here**\n\nIt breaks because of the reason given here.\n\n" +
			"<!-- devin-review-badge-begin -->\n<a href=\"https://app.devin.ai/x\">x</a>\n<!-- devin-review-badge-end -->\n\n" +
			"---\n*Was this helpful? React with 👍 or 👎 to provide feedback.*\n"
	)
	cases := []struct {
		login, body string
		want        external.Tool
	}{
		{"coderabbitai[bot]", "_⚠️ Potential issue_ | _🟠 Major_\n\n**Bad thing happens here.**", external.ToolCodeRabbit},
		{"coderabbitai[bot]", "**<picture><img src=\"https://x/amber.svg\" width=\"16\" height=\"16\" alt=\"\"></picture> Minor** · **Functional Correctness** · **Quick win**\n\n**Bad thing happens here.**", external.ToolCodeRabbit},
		{"copilot-pull-request-reviewer[bot]", copilotProse, external.ToolCopilot},
		{"Copilot", copilotProse, external.ToolCopilot},
		{"cursor[bot]", "### Bad thing\n\n**Low Severity**\n\n<!-- DESCRIPTION START -->\nBad thing happens.\n<!-- DESCRIPTION END -->", external.ToolBugbot},
		{"greptile-apps[bot]", "**logic:** bad thing happens on this line of the file.", external.ToolGreptile},
		{"greptile-apps[bot]", "duplicate resize listener - the listener is registered twice, so two calls happen.", external.ToolGreptile},
		{"macroscopeapp[bot]", "🟠 **High** `src/a.ts:12`\n\nBad thing happens here, and the reason is long enough.", external.ToolMacroscope},
		{"chatgpt-codex-connector[bot]", codexBody, external.ToolCodex},
		{"devin-ai-integration[bot]", devinBody, external.ToolDevin},
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
	// The Codex badge is required: the login alone is not the connector's mark.
	c = inline(1, "a.go", 3)
	c.UserLogin, c.Body = "chatgpt-codex-connector[bot]", copilotProse
	require.Equal(t, external.IgnoreNoMarker, newPR().Ingest(c).Ignore)
	// Devin's payload or badge is required.
	c = inline(1, "a.go", 3)
	c.UserLogin, c.Body = "devin-ai-integration[bot]", copilotProse
	require.Equal(t, external.IgnoreNoMarker, newPR().Ingest(c).Ignore)
}

// TestCopilotIsListedByEitherLogin: a policy written from the older documentation names
// copilot-pull-request-reviewer[bot] and the real review is posted by "Copilot", and the other way round. Either
// listing reads either login.
func TestCopilotIsListedByEitherLogin(t *testing.T) {
	const body = "The error from Close is dropped here, so a failed flush is lost."
	for _, listed := range []string{"copilot-pull-request-reviewer[bot]", "Copilot", "copilot"} {
		for _, login := range []string{"Copilot", "copilot-pull-request-reviewer[bot]"} {
			c := inline(1, "a.go", 3)
			c.UserLogin, c.Body = login, body
			got := external.PR{Head: head, Sources: []string{listed}}.Ingest(c)
			require.Equal(t, external.OutcomeCandidate, got.Outcome, "listed %q, login %q: %+v", listed, login, got)
			require.Equal(t, external.ToolCopilot, got.Tool)
		}
	}
	// A login of another tool is still not admitted by a Copilot listing.
	c := inline(1, "a.go", 3)
	got := external.PR{Head: head, Sources: []string{"Copilot"}}.Ingest(c)
	require.Equal(t, external.IgnoreNotInSources, got.Ignore)
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

// TestClaimLosesTheToolNoise: the tool's own labels never open a claim, and the severity it states is kept. The bodies
// are the real formats, shortened.
func TestClaimLosesTheToolNoise(t *testing.T) {
	cases := []struct {
		name, login, body string
		sev               string
		stated            bool
		want              string
	}{
		{
			name:  "macroscope: the severity, the path and the footer",
			login: "macroscopeapp[bot]",
			body:  "🟠 **High** `src/git.ts:45`\n\nThe fallback is unreachable.\n\nNo longer relevant as of 79fdda9deea9abd21c3036f6a1d1f5004db4a8b2\n",
			sev:   "high", stated: true, want: "The fallback is unreachable.",
		},
		{
			name:  "macroscope: the path, its line and the resolved footer",
			login: "macroscopeapp[bot]",
			body:  "🟢 **Low** `components/Chat.tsx:413`\n\nAdd the missing field.\n\n✅ Resolved in 4855d5ca6bc91a44e5757f1830070689414de0ec\n",
			sev:   "low", stated: true, want: "Add the missing field.",
		},
		{
			name:  "macroscope: a blob link instead of a path, and the fix prompt",
			login: "macroscopeapp[bot]",
			body:  "🟡 **Medium**\n\nhttps://github.com/o/r/blob/81eabe2f7b436dafb24a20d19ae9fd1724032fb6/apps/web/src/A.tsx#L40\n\nThe stage is never shown.\n\n> 🚀 **Want me to fix this? Reply ex: _\"fix it for me\"_.**\n",
			sev:   "medium", stated: true, want: "The stage is never shown.",
		},
		{
			name:  "macroscope: no severity in the body",
			login: "macroscopeapp[bot]",
			body:  "`src/main.ts:333`\n\nTwo names collide.\n",
			sev:   "medium", stated: false, want: "Two names collide.",
		},
		{
			name:  "macroscope: the MURMUR_IGNORE marker and the footer",
			login: "macroscopeapp[bot]",
			body:  "<!-- MURMUR_IGNORE -->\n🟡 **Medium** `src/a.ts:136`\n\nThe pattern is too wide.\n\n✅ Resolved in 536dc179add4c08b27b331475cf051bf225243f1\n",
			sev:   "medium", stated: true, want: "The pattern is too wide.",
		},
		{
			name:  "codex: the P badge is the severity and leaves the claim",
			login: "chatgpt-codex-connector[bot]",
			body:  "**<sub><sub>![P1 Badge](https://img.shields.io/badge/P1-orange?style=flat)</sub></sub>  Do not serialize startup**\n\nIt waits for the title call.\n\nUseful? React with 👍 / 👎.",
			sev:   "high", stated: true, want: "Do not serialize startup. It waits for the title call.",
		},
		{
			name:  "codex: P2 is medium",
			login: "chatgpt-codex-connector[bot]",
			body:  "**<sub><sub>![P2 Badge](https://img.shields.io/badge/P2-yellow?style=flat)</sub></sub>  Preserve the mode**\n\nThe mode is reset too early.",
			sev:   "medium", stated: true, want: "Preserve the mode. The mode is reset too early.",
		},
		{
			name:  "devin: the emoji is the severity and the payload is gone",
			login: "devin-ai-integration[bot]",
			body:  "<!-- devin-review-comment {\"id\": \"BUG_1\", \"file_path\": \"a.go\"} -->\n\n🔴 **Unhandled exception in the catch block**\n\nThe second send throws.\n\n<details><summary>Root Cause</summary>\n\nLong text.\n\n</details>\n\n---\n*Was this helpful? React with 👍 or 👎 to provide feedback.*\n",
			sev:   "critical", stated: true, want: "Unhandled exception in the catch block. The second send throws.",
		},
		{
			name:  "coderabbit: the November header is read and removed",
			login: "coderabbitai[bot]",
			body:  "**<picture><img src=\"https://x/amber.svg\" width=\"16\" height=\"16\" alt=\"\"></picture> Major** · **Data Integrity & Integration** · **Heavy lift**\n\n**Refresh the boundary.**\n\nA stale key replays.",
			sev:   "high", stated: true, want: "Refresh the boundary. A stale key replays.",
		},
		{
			name:  "coderabbit: the November header with a Minor severity",
			login: "coderabbitai[bot]",
			body:  "**<picture><img src=\"https://x/amber.svg\" width=\"16\" height=\"16\" alt=\"\"></picture> Minor** · **Functional Correctness** · **Quick win**\n\nPreserve the paste context.",
			sev:   "medium", stated: true, want: "Preserve the paste context.",
		},
		{
			name:  "a bold title line that is not bold ends with a full stop",
			login: "coderabbitai[bot]",
			body:  "_⚠️ Potential issue_ | _🟠 Major_\n\n**The value is not checked**\n\nIt can be empty here.",
			sev:   "high", stated: true, want: "The value is not checked. It can be empty here.",
		},
		{
			name:  "greptile: plain prose with no marker",
			login: "greptile-apps[bot]",
			body:  "duplicate resize listener - the listener is registered twice.\n\n```suggestion\nconst t = 1;\n```\n",
			sev:   "medium", stated: false, want: "duplicate resize listener - the listener is registered twice.",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := inline(1, "a.go", 3)
			c.UserLogin, c.Body = tc.login, tc.body
			got := newPR().Ingest(c)
			require.Equal(t, external.OutcomeCandidate, got.Outcome, "%+v", got)
			require.Equal(t, tc.want, got.Candidate.Claim)
			require.Equal(t, tc.sev, string(got.Candidate.Severity))
			require.Equal(t, tc.stated, got.Candidate.SeverityStated)
		})
	}
}

// TestARefusalIsNotAClaim: Macroscope answers some requests with a permission refusal. That is a fact about the bot's
// access, not a claim about the diff, so it is ignored as no_claim and never reaches the proof run.
func TestARefusalIsNotAClaim(t *testing.T) {
	for _, body := range []string{
		"Sorry, I'm unable to act on this request because you do not have permissions within this repository.",
		"Sorry, I'm unable to act on this request because you do not have permissions within this repository.\n<!-- macroscope-meta: {\"kind\":\"pr_assistant\"} -->",
	} {
		c := inline(1, "a.go", 3)
		c.UserLogin, c.Body = "macroscopeapp[bot]", body
		got := newPR().Ingest(c)
		require.Equal(t, external.OutcomeIgnored, got.Outcome, "%+v", got)
		require.Equal(t, external.IgnoreNoClaim, got.Ignore)
	}
}
