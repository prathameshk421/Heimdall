package external

import (
	"regexp"
	"strings"
)

// Tool is a third-party review tool this package can read.
type Tool string

// The four tools of ACTION_MODE 6.1.
const (
	ToolCodeRabbit Tool = "coderabbit"
	ToolCopilot    Tool = "copilot"
	ToolBugbot     Tool = "cursor_bugbot"
	ToolGreptile   Tool = "greptile"
)

var (
	reCodeRabbitHeader = regexp.MustCompile(`(?m)^[ \t]*_([^|\n]{1,80})_[ \t]*\|[ \t]*_([^\n]{1,80})_[ \t]*$`)
	reBugbotSeverity   = regexp.MustCompile(`(?mi)^[ \t]*\*\*(Critical|High|Medium|Low) Severity\*\*[ \t]*$`)
	reGreptilePrefix   = regexp.MustCompile(`^\s*\*\*(logic|syntax|style)\s*:\*\*[ \t]*`)
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
	}
	return ""
}

// hasMarker says whether the body carries a mark of the tool. Copilot writes plain prose in its inline comments and has
// no fixed mark, so for it the login and the Bot type are all there is.
func hasMarker(t Tool, body string) bool {
	lower := strings.ToLower(body)
	switch t {
	case ToolCodeRabbit:
		return reCodeRabbitHeader.MatchString(body) || strings.Contains(lower, "coderabbit") ||
			strings.Contains(lower, "fingerprinting:phantom")
	case ToolBugbot:
		return strings.Contains(body, "BUGBOT_BUG_ID") || strings.Contains(body, "<!-- DESCRIPTION START -->") ||
			reBugbotSeverity.MatchString(body)
	case ToolGreptile:
		return reGreptilePrefix.MatchString(body) || strings.Contains(lower, "greptile") ||
			strings.Contains(lower, "prompt to fix with ai")
	case ToolCopilot:
		return true
	}
	return false
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

func (pr PR) listed(login string) bool {
	want := normLogin(login)
	for _, s := range pr.Sources {
		if normLogin(s) == want {
			return true
		}
	}
	return false
}
