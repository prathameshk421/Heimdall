package external

import (
	"regexp"
	"strings"
)

// Tool is a third-party review tool this package can read.
type Tool string

// The tools. The first four are the sources of ACTION_MODE 6.1. The last three are the logins that post inline findings
// on the repositories this package was measured on (docs/product/research/13-t3code-bot-comments.md, 182 of 383 real
// comments), and a policy that lists one of them gets it read.
const (
	ToolCodeRabbit Tool = "coderabbit"
	ToolCopilot    Tool = "copilot"
	ToolBugbot     Tool = "cursor_bugbot"
	ToolGreptile   Tool = "greptile"
	ToolMacroscope Tool = "macroscope"
	ToolCodex      Tool = "codex"
	ToolDevin      Tool = "devin"
)

var (
	reCodeRabbitHeader = regexp.MustCompile(`(?m)^[ \t]*_([^|\n]{1,80})_[ \t]*\|[ \t]*_([^\n]{1,80})_[ \t]*$`)
	// reCodeRabbitBold is the header CodeRabbit writes from November 2026: "**<picture>...</picture> Minor** ·
	// **Functional Correctness** · **Quick win**". The icon is optional, so the same pattern reads the raw body and
	// the text after the HTML clean-up, and the whole line is the header.
	reCodeRabbitBold = regexp.MustCompile(`(?m)^[ \t]*\*\*(?:<picture>.*?</picture>)?[ \t]*(Critical|Major|Minor|Trivial|Nitpick)[ \t]*\*\*[ \t]*(?:·.*)?$`)
	reBugbotSeverity = regexp.MustCompile(`(?mi)^[ \t]*\*\*(Critical|High|Medium|Low) Severity\*\*[ \t]*$`)
	reGreptilePrefix = regexp.MustCompile(`^\s*\*\*(logic|syntax|style)\s*:\*\*[ \t]*`)
)

// normLogin turns a login into the name that is compared: REST gives "coderabbitai[bot]" and GraphQL gives
// "coderabbitai", and GitHub logins do not depend on case.
func normLogin(login string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(login)), "[bot]")
}

// toolFor names the tool that owns a login, or "" for any other author.
func toolFor(login string) Tool {
	switch normLogin(login) {
	case "coderabbitai":
		return ToolCodeRabbit
	case "copilot-pull-request-reviewer", "copilot":
		return ToolCopilot
	case "cursor":
		return ToolBugbot
	case "greptile-apps":
		return ToolGreptile
	case "macroscopeapp":
		return ToolMacroscope
	case "chatgpt-codex-connector":
		return ToolCodex
	case "devin-ai-integration":
		return ToolDevin
	}
	return ""
}

// hasMarker says whether the body carries a mark of the tool. The login is enough for Copilot, Greptile and Macroscope:
// their inline comments are plain prose with no fixed mark at all, which is what the measured repository shows.
func hasMarker(t Tool, body string) bool {
	switch t {
	case ToolCodeRabbit:
		return codeRabbitMark(body)
	case ToolBugbot:
		return strings.Contains(body, "BUGBOT_BUG_ID") || strings.Contains(body, "<!-- DESCRIPTION START -->") ||
			reBugbotSeverity.MatchString(body)
	case ToolCodex:
		return reCodexBadge.MatchString(body) || strings.Contains(strings.ToLower(body), "useful? react with")
	case ToolDevin:
		return strings.Contains(body, "devin-review-comment") || strings.Contains(body, "devin-review-badge")
	case ToolCopilot, ToolGreptile, ToolMacroscope:
		return true
	}
	return false
}

// codeRabbitMark is a CodeRabbit header of either era, the tool's own name, or its fingerprinting footer.
func codeRabbitMark(body string) bool {
	lower := strings.ToLower(body)
	return reCodeRabbitHeader.MatchString(body) || reCodeRabbitBold.MatchString(body) ||
		strings.Contains(lower, "coderabbit") || strings.Contains(lower, "fingerprinting:phantom")
}

// recognise decides whether a comment is one of ours: a Bot, a login of a known tool, listed in the policy sources,
// the first comment of a thread, and with the mark of its tool.
func (pr PR) recognise(c Comment, body string) (Tool, IgnoreReason) {
	if !strings.EqualFold(c.UserType, "Bot") {
		return "", IgnoreNotBot
	}
	tool := toolFor(c.UserLogin)
	if tool == "" {
		return "", IgnoreUnknownAuthor
	}
	if !pr.listed(c.UserLogin) {
		return tool, IgnoreNotInSources
	}
	if c.InReplyTo != 0 {
		return tool, IgnoreReply
	}
	if !hasMarker(tool, body) {
		return tool, IgnoreNoMarker
	}
	return tool, ""
}

// listed says whether the policy lists the author of a comment. A tool with more than one login is listed by any of
// them: Copilot posts as "Copilot" and as "copilot-pull-request-reviewer[bot]", and a policy written from the older
// documentation must still read the newer one.
func (pr PR) listed(login string) bool {
	want, wantTool := normLogin(login), toolFor(login)
	for _, s := range pr.Sources {
		if normLogin(s) == want || (wantTool != "" && toolFor(s) == wantTool) {
			return true
		}
	}
	return false
}
