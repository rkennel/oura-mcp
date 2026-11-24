package main

import (
	"slices"
	"strings"
	"testing"
)

func TestParseRulesReadsIDsTitlesAndScriptMarks(t *testing.T) {
	text := "# Guide\n\nIntro with a list:\n\n- not a rule, above any section\n\n## Standards\n\n" +
		"- `first-rule` **First rule.** Text that wraps\n  onto a second line.\n" +
		"- `second-rule` **Second.** Text whose mark wraps: *Checked by\n  `scripts/check-standards.sh`.*\n\n" +
		"## More\n\n- `third` **Third** with no full stop.\n"

	rules, err := parseRules(text)
	if err != nil {
		t.Fatalf("parseRules: %v", err)
	}

	want := []rule{
		{ID: "first-rule", Title: "First rule"},
		{ID: "second-rule", Title: "Second", Script: true},
		{ID: "third", Title: "Third"},
	}
	if !slices.Equal(rules, want) {
		t.Errorf("rules = %+v, want %+v", rules, want)
	}
	if got := ruleIDs(rules, true); !slices.Equal(got, []string{"second-rule"}) {
		t.Errorf("script rules = %v", got)
	}
}

func TestParseRulesRejectsRulesWithoutUniqueIDs(t *testing.T) {
	for name, text := range map[string]string{
		"no ID":        "## Rules\n\n- **A rule.** With no ID.\n",
		"duplicate ID": "## Rules\n\n- `a` **A.** One.\n- `a` **A again.** Two.\n",
		"no rules":     "# Guide\n\nNothing here.\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseRules(text); err == nil {
				t.Error("parseRules succeeded")
			}
		})
	}
}

func TestLoadRulesReportsAMissingFile(t *testing.T) {
	if _, err := loadRules(t.TempDir() + "/GEMINI.md"); err == nil || !strings.Contains(err.Error(), "read the standards") {
		t.Errorf("err = %v", err)
	}
}

// The repository's own GEMINI.md must parse, and mark as checked by the
// script exactly the rules the standards check covers, so that no rule is
// left without a verdict and none is judged twice.
func TestTheRepositoryStandardsMatchTheStandardsCheck(t *testing.T) {
	rules, err := loadRules("../../GEMINI.md")
	if err != nil {
		t.Fatalf("loadRules: %v", err)
	}
	script := ruleIDs(rules, true)
	slices.Sort(script)
	checked := slices.Clone(checkedRules)
	slices.Sort(checked)
	if !slices.Equal(script, checked) {
		t.Errorf("GEMINI.md marks %v as checked by the script, but the check covers %v", script, checked)
	}
	if len(ruleIDs(rules, false)) == 0 {
		t.Error("GEMINI.md leaves no rule for Gemini")
	}
}
