package jira

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// adfNode is one node of an Atlassian Document Format tree (REST v3 returns descriptions in this form, V52).
type adfNode struct {
	Type    string    `json:"type"`
	Text    string    `json:"text"`
	Attrs   adfAttrs  `json:"attrs"`
	Content []adfNode `json:"content"`
}

// adfAttrs holds the attributes this walker reads: a heading level, a mention or emoji text, and a link url.
type adfAttrs struct {
	Level int    `json:"level"`
	Text  string `json:"text"`
	URL   string `json:"url"`
}

// descriptionText renders an ADF description as markdown-like lines that the criteria parser reads: headings as
// "#" lines, bullets as "- ", numbered items as "1. ", nested items indented by two spaces, and a blank line between
// top-level blocks. A missing or null description is empty. Anything that is not ADF is refused, because REST v2
// wiki markup would be read wrongly (V52).
func descriptionText(raw json.RawMessage) (string, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return "", nil
	}
	var doc adfNode
	if err := json.Unmarshal(trimmed, &doc); err != nil {
		return "", fmt.Errorf("jira description is not ADF: %w", err)
	}
	if doc.Type != "doc" {
		return "", fmt.Errorf("jira description root is %q, not doc", doc.Type)
	}
	return strings.Trim(strings.Join(blocks(doc.Content), "\n"), "\n"), nil
}

// blocks renders top-level blocks with a blank line between the ones that produced text.
func blocks(nodes []adfNode) []string {
	var out []string
	for _, n := range nodes {
		lines := block(n)
		if len(lines) == 0 {
			continue
		}
		if len(out) > 0 {
			out = append(out, "")
		}
		out = append(out, lines...)
	}
	return out
}

// block renders one block node.
func block(n adfNode) []string {
	switch n.Type {
	case "heading":
		text := inline(n.Content)
		if text == "" {
			return nil
		}
		level := min(max(n.Attrs.Level, 1), 6)
		return []string{strings.Repeat("#", level) + " " + text}
	case "paragraph", "codeBlock":
		if text := inline(n.Content); text != "" {
			return []string{text}
		}
		return nil
	case "bulletList", "orderedList":
		return list(n, 0)
	case "text":
		if strings.TrimSpace(n.Text) != "" {
			return []string{strings.TrimSpace(n.Text)}
		}
		return nil
	default:
		// Containers such as blockquote and panel keep their text; a block with no text renders nothing.
		return blocks(n.Content)
	}
}

// list renders a bullet or numbered list at a nesting depth. An item with no text still shows its children.
func list(n adfNode, depth int) []string {
	indent := strings.Repeat("  ", depth)
	var out []string
	number := 0
	for _, item := range n.Content {
		if item.Type != "listItem" {
			continue
		}
		number++
		marker := "- "
		if n.Type == "orderedList" {
			marker = strconv.Itoa(number) + ". "
		}
		text, nested := itemParts(item, depth)
		if text != "" {
			out = append(out, indent+marker+text)
		}
		out = append(out, nested...)
	}
	return out
}

// itemParts splits a list item into its own text and the lines of its nested blocks.
func itemParts(item adfNode, depth int) (string, []string) {
	var texts []string
	var nested []string
	for _, child := range item.Content {
		switch child.Type {
		case "paragraph", "heading":
			if t := inline(child.Content); t != "" {
				texts = append(texts, t)
			}
		case "bulletList", "orderedList":
			nested = append(nested, list(child, depth+1)...)
		default:
			for _, line := range block(child) {
				nested = append(nested, strings.Repeat("  ", depth+1)+line)
			}
		}
	}
	return strings.Join(texts, " "), nested
}

// inline returns the visible text of inline nodes, trimmed.
func inline(nodes []adfNode) string {
	return strings.TrimSpace(inlineRaw(nodes))
}

// inlineRaw concatenates inline text without trimming, so spaces between runs survive.
func inlineRaw(nodes []adfNode) string {
	var b strings.Builder
	for _, n := range nodes {
		switch n.Type {
		case "text":
			b.WriteString(n.Text)
		case "hardBreak":
			b.WriteString(" ")
		case "mention", "emoji":
			b.WriteString(n.Attrs.Text)
		case "inlineCard":
			b.WriteString(n.Attrs.URL)
		default:
			b.WriteString(inlineRaw(n.Content))
		}
	}
	return b.String()
}
