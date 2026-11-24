package main

import (
	"strings"
	"testing"
)

var testRules = []rule{
	{ID: "test-driven", Title: "Test-driven"},
	{ID: "coverage", Title: "Coverage"},
	{ID: "config-in-app", Title: "Configuration in one place", Script: true},
}

var allChecked = standardsResult{Rules: []string{"config-in-app"}}

func TestAssembleReviewGivesEveryRuleAVerdict(t *testing.T) {
	reply := geminiReview{
		Summary:  "Adds a helper.",
		Comments: []geminiComment{{Path: "internal/a.go", Line: 3, Body: "No test."}},
		Checklist: []checklistEntry{
			{Rule: "coverage", Verdict: "pass", Reason: "internal/ is at 98.5%."},
			{Rule: " test-driven ", Verdict: " FAIL ", Reason: " helper has no test "},
			{Rule: "config-in-app", Verdict: "made up", Reason: ""},
		},
	}
	standards := standardsResult{Rules: []string{"config-in-app"}, Findings: []finding{
		{Rule: "config-in-app", Path: "internal/oura/client.go", Line: 6, Side: sideRight, Message: "os.Getenv reads the environment"},
	}}

	review, err := assembleReview(reply, testRules, standards)
	if err != nil {
		t.Fatalf("assembleReview: %v", err)
	}

	want := []struct{ id, verdict, reason string }{
		{"test-driven", verdictFail, "helper has no test"},
		{"coverage", verdictPass, "internal/ is at 98.5%."},
		{"config-in-app", verdictFail, "scripts/check-standards.sh found 1: `internal/oura/client.go` line 6."},
	}
	if len(review.Checklist) != len(want) {
		t.Fatalf("checklist = %+v", review.Checklist)
	}
	for i, w := range want {
		row := review.Checklist[i]
		if row.Rule.ID != w.id || row.Verdict != w.verdict || row.Reason != w.reason {
			t.Errorf("row %d = %+v, want %+v", i, row, w)
		}
	}
	if len(review.Comments) != 2 || !strings.Contains(review.Comments[1].Body, "**[STANDARDS]** `config-in-app`") ||
		review.Comments[1].Path != "internal/oura/client.go" || review.Comments[1].Line != 6 {
		t.Errorf("comments = %+v, want Gemini's then the finding", review.Comments)
	}
	if !review.hasFindings() || review.verdictFor("coverage").Verdict != verdictPass || review.verdictFor("nope").Verdict != "" {
		t.Error("hasFindings or verdictFor is wrong")
	}
	if got := failedRules(review.Checklist); strings.Join(got, ",") != "config-in-app,test-driven" {
		t.Errorf("failedRules = %v", got)
	}
}

func TestAssembleReviewPassesAMechanicalRuleWithNoFindings(t *testing.T) {
	reply := geminiReview{Summary: "Fine.", Checklist: []checklistEntry{
		{Rule: "test-driven", Verdict: "pass", Reason: "Tested."},
		{Rule: "coverage", Verdict: "not_applicable", Reason: "Docs only."},
	}}

	review, err := assembleReview(reply, testRules, allChecked)
	if err != nil {
		t.Fatalf("assembleReview: %v", err)
	}
	if row := review.verdictFor("config-in-app"); row.Verdict != verdictPass || !strings.Contains(row.Reason, "no breach") {
		t.Errorf("config-in-app = %+v", row)
	}
	if review.hasFindings() {
		t.Error("a review with no comments and no failed rule has findings")
	}
}

func TestAssembleReviewRejectsAnIncompleteOrMalformedChecklist(t *testing.T) {
	pass := func(id string) checklistEntry { return checklistEntry{Rule: id, Verdict: "pass", Reason: "Fine."} }
	for name, tc := range map[string]struct {
		checklist []checklistEntry
		standards standardsResult
		want      string
	}{
		"a rule omitted":      {[]checklistEntry{pass("coverage")}, allChecked, "omits the rules test-driven"},
		"no checklist at all": {nil, allChecked, "omits the rules test-driven, coverage"},
		"an unknown rule":     {[]checklistEntry{pass("coverage"), pass("test-driven"), pass("made-up")}, allChecked, `"made-up", which is not a rule`},
		"a bad verdict": {[]checklistEntry{pass("coverage"), {Rule: "test-driven", Verdict: "maybe", Reason: "x"}},
			allChecked, `"maybe", not pass`},
		"no reason": {[]checklistEntry{pass("coverage"), {Rule: "test-driven", Verdict: "pass"}},
			allChecked, "gives no reason"},
		"a rule judged twice": {[]checklistEntry{pass("coverage"), pass("test-driven"), pass("coverage")},
			allChecked, "judges coverage twice"},
		"a mechanical rule the check missed": {[]checklistEntry{pass("coverage"), pass("test-driven")},
			standardsResult{}, "marks config-in-app as checked"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := assembleReview(geminiReview{Summary: "x", Checklist: tc.checklist}, testRules, tc.standards)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}

func TestChecklistTableShowsEveryVerdictAndReason(t *testing.T) {
	table := checklistTable([]verdictRow{
		{Rule: testRules[0], Verdict: verdictPass, Reason: "Tested |\nwell."},
		{Rule: testRules[1], Verdict: verdictFail, Reason: "At 63.4%."},
		{Rule: testRules[2], Verdict: verdictNotApplicable, Reason: "No config."},
	})

	for _, part := range []string{
		"| Rule | Verdict | Reason |",
		"| `test-driven` Test-driven | ✅ pass | Tested \\| well. |",
		"| `coverage` Coverage | ❌ fail | At 63.4%. |",
		"| `config-in-app` Configuration in one place | ➖ not applicable | No config. |",
	} {
		if !strings.Contains(table, part) {
			t.Errorf("table lacks %q:\n%s", part, table)
		}
	}
}

func TestReviewBodyShowsTheChecklistAndNoFindingsNoteOnlyWhenClean(t *testing.T) {
	failed := assembledReview{Summary: "Coverage dropped.", Checklist: []verdictRow{{Rule: testRules[1], Verdict: verdictFail, Reason: "63.4%"}}}
	body := reviewBody(failed, nil, "abc", "")

	if strings.Contains(body, noFindings) {
		t.Errorf("a review with a failed rule says nothing was found:\n%s", body)
	}
	if !strings.Contains(body, "### Standards checklist") || !strings.Contains(body, "❌ fail") {
		t.Errorf("body lacks the checklist:\n%s", body)
	}
}
