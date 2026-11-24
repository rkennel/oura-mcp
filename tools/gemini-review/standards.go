package main

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// scriptMark is how GEMINI.md marks a rule that scripts/check-standards.sh
// checks, so that the review takes the rule's verdict from the script.
const scriptMark = "Checked by `scripts/check-standards.sh`"

// rule is one rule of GEMINI.md.
type rule struct {
	ID     string
	Title  string
	Script bool // checked by scripts/check-standards.sh, not by Gemini
}

// ruleBullet matches the first line of a rule: "- `id` **Title.** text".
var ruleBullet = regexp.MustCompile("^- `([a-z][a-z0-9-]*)` \\*\\*(.+?)\\*\\*")

// loadRules reads the rules from the standards file, GEMINI.md.
func loadRules(file string) ([]rule, error) {
	raw, err := os.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("read the standards: %w", err)
	}
	rules, err := parseRules(string(raw))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	return rules, nil
}

// parseRules reads every rule under a "## " heading. Each bullet there is a
// rule and must start with its ID, so that a rule cannot be added without
// one and then go unchecked. A bullet runs until the next bullet, blank line
// or heading.
func parseRules(text string) ([]rule, error) {
	var rules []rule
	var bullet []string
	seen := map[string]bool{}
	inSection := false

	finish := func() error {
		if len(bullet) == 0 {
			return nil
		}
		parsed, err := parseRule(bullet)
		bullet = nil
		if err != nil {
			return err
		}
		if seen[parsed.ID] {
			return fmt.Errorf("rule ID %q is used twice", parsed.ID)
		}
		seen[parsed.ID] = true
		rules = append(rules, parsed)
		return nil
	}

	for _, line := range strings.Split(text, "\n") {
		startsBlock := strings.HasPrefix(line, "#") || strings.HasPrefix(line, "- ") || strings.TrimSpace(line) == ""
		if startsBlock {
			if err := finish(); err != nil {
				return nil, err
			}
		}
		switch {
		case strings.HasPrefix(line, "## "):
			inSection = true
		case strings.HasPrefix(line, "# "):
			inSection = false
		case inSection && strings.HasPrefix(line, "- "):
			bullet = []string{line}
		case len(bullet) > 0:
			bullet = append(bullet, strings.TrimSpace(line))
		}
	}
	if err := finish(); err != nil {
		return nil, err
	}
	if len(rules) == 0 {
		return nil, errors.New("no rules found")
	}
	return rules, nil
}

func parseRule(lines []string) (rule, error) {
	match := ruleBullet.FindStringSubmatch(lines[0])
	if match == nil {
		return rule{}, fmt.Errorf("rule %q does not start with an ID such as - `rule-id` **Title.**", lines[0])
	}
	text := strings.Join(lines, " ")
	return rule{
		ID:     match[1],
		Title:  strings.TrimSuffix(match[2], "."),
		Script: strings.Contains(text, scriptMark),
	}, nil
}

// ruleIDs returns the IDs of the rules that script says are checked by the
// script (true) or by Gemini (false).
func ruleIDs(rules []rule, script bool) []string {
	var ids []string
	for _, r := range rules {
		if r.Script == script {
			ids = append(ids, r.ID)
		}
	}
	return ids
}
