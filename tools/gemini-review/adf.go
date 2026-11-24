package main

import (
	"fmt"
	"regexp"
	"strings"
)

// adfNode is a node of the Atlassian Document Format, the JSON tree Jira
// keeps rich text in.
type adfNode struct {
	Type    string         `json:"type"`
	Text    string         `json:"text,omitempty"`
	Attrs   map[string]any `json:"attrs,omitempty"`
	Marks   []adfMark      `json:"marks,omitempty"`
	Content []adfNode      `json:"content,omitempty"`
}

type adfMark struct {
	Type string `json:"type"`
}

// renderADF renders a document as Markdown, close enough for Gemini to read
// its headings, emphasis and lists. A missing document renders as "".
func renderADF(doc *adfNode) string {
	if doc == nil {
		return ""
	}
	return strings.TrimSpace(renderBlocks(doc.Content))
}

func renderBlocks(nodes []adfNode) string {
	var blocks []string
	for _, node := range nodes {
		if block := strings.TrimSpace(renderBlock(node)); block != "" {
			blocks = append(blocks, block)
		}
	}
	return strings.Join(blocks, "\n\n")
}

func renderBlock(node adfNode) string {
	switch node.Type {
	case "paragraph":
		return renderInline(node.Content)
	case "heading":
		return strings.Repeat("#", headingLevel(node)) + " " + renderInline(node.Content)
	case "bulletList", "orderedList":
		return renderList(node)
	case "codeBlock":
		return "```\n" + renderInline(node.Content) + "\n```"
	case "blockquote":
		return "> " + strings.ReplaceAll(renderBlocks(node.Content), "\n", "\n> ")
	case "rule":
		return "---"
	case "text", "hardBreak", "mention", "emoji", "inlineCard", "date", "status":
		return renderInline([]adfNode{node})
	default:
		// Panels, tables, expands and the like: their content still reads.
		return renderBlocks(node.Content)
	}
}

func renderList(list adfNode) string {
	var b strings.Builder
	for i, item := range list.Content {
		marker := "- "
		if list.Type == "orderedList" {
			marker = fmt.Sprintf("%d. ", i+1)
		}
		body := strings.ReplaceAll(renderBlocks(item.Content), "\n", "\n   ")
		b.WriteString(marker + body + "\n")
	}
	return b.String()
}

func renderInline(nodes []adfNode) string {
	var b strings.Builder
	for _, node := range nodes {
		switch node.Type {
		case "text":
			b.WriteString(withMarks(node.Text, node.Marks))
		case "hardBreak":
			b.WriteString("\n")
		default:
			if text := attrText(node); text != "" {
				b.WriteString(text)
			} else {
				b.WriteString(renderInline(node.Content))
			}
		}
	}
	return b.String()
}

// attrText is the text an inline node such as a mention or a link card
// keeps in its attributes.
func attrText(node adfNode) string {
	for _, name := range []string{"text", "shortName", "url"} {
		if text, ok := node.Attrs[name].(string); ok && text != "" {
			return text
		}
	}
	return ""
}

func withMarks(text string, marks []adfMark) string {
	for _, mark := range marks {
		switch mark.Type {
		case "strong":
			text = "**" + text + "**"
		case "em":
			text = "*" + text + "*"
		case "code":
			text = "`" + text + "`"
		}
	}
	return text
}

func headingLevel(node adfNode) int {
	if level, ok := node.Attrs["level"].(float64); ok && level >= 1 && level <= 6 {
		return int(level)
	}
	return 1
}

// criteriaHeading matches the heading the acceptance criteria sit under.
var criteriaHeading = regexp.MustCompile(`(?i)^acceptance criteria:?$`)

// acceptanceCriteria reads the acceptance criteria out of a description:
// what follows a heading, or a paragraph on its own, that says "Acceptance
// criteria", up to the next heading no deeper than it. Each paragraph is one
// criterion, as in a Given/When/Then scenario, and so is each list item.
func acceptanceCriteria(doc *adfNode) []string {
	if doc == nil {
		return nil
	}
	start, level := criteriaStart(doc.Content)
	if start < 0 {
		return nil
	}
	var criteria []string
	for _, node := range doc.Content[start+1:] {
		if node.Type == "heading" && headingLevel(node) <= level {
			break
		}
		criteria = append(criteria, criteriaIn(node)...)
	}
	return criteria
}

// criteriaStart finds the node that opens the acceptance criteria and the
// heading level that would close them. A paragraph used as a heading is
// closed by any heading.
func criteriaStart(nodes []adfNode) (int, int) {
	for i, node := range nodes {
		if !criteriaHeading.MatchString(strings.TrimSpace(plainText(node))) {
			continue
		}
		switch node.Type {
		case "heading":
			return i, headingLevel(node)
		case "paragraph":
			return i, 6
		}
	}
	return -1, 0
}

func criteriaIn(node adfNode) []string {
	var criteria []string
	switch node.Type {
	case "heading", "rule":
		// A deeper heading groups criteria; it is not one.
	case "bulletList", "orderedList":
		for _, item := range node.Content {
			criteria = append(criteria, strings.TrimSpace(renderBlocks(item.Content)))
		}
	default:
		criteria = append(criteria, strings.TrimSpace(renderBlock(node)))
	}
	var kept []string
	for _, c := range criteria {
		if c != "" {
			kept = append(kept, c)
		}
	}
	return kept
}

// plainText is a node's text without marks.
func plainText(node adfNode) string {
	if node.Type == "text" {
		return node.Text
	}
	var b strings.Builder
	for _, child := range node.Content {
		b.WriteString(plainText(child))
	}
	return b.String()
}
