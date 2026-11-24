package main

import (
	"strings"
	"testing"
)

func TestAssembleReviewNumbersTheFindingsInOrder(t *testing.T) {
	review, err := assembleReview(geminiReview{
		Summary:  "Two.",
		Comments: []geminiComment{{Path: "a.go", Line: 1, Body: "One."}, {Path: "b.go", Line: 2, Body: "Two."}},
	}, nil, standardsResult{Findings: []finding{{Rule: "config-in-app", Path: "c.go", Line: 3, Message: "Reads the environment"}}})
	if err != nil {
		t.Fatalf("assembleReview: %v", err)
	}

	var ids []string
	for _, c := range review.Comments {
		ids = append(ids, c.ID)
	}
	if strings.Join(ids, ",") != "F1,F2,F3" {
		t.Errorf("IDs = %v, want F1, F2 and F3, the standards finding last", ids)
	}
}

func TestFindingLabelShowsSeverityAndID(t *testing.T) {
	for _, tc := range []struct {
		comment geminiComment
		want    string
	}{
		{geminiComment{Severity: "high", ID: "F2"}, "**[HIGH] F2**"},
		{geminiComment{Severity: "concern", ID: "F1"}, "**[CONCERN] F1**"},
		{geminiComment{ID: "F3"}, "**F3**"},
		{geminiComment{Severity: "low"}, "**[LOW]**"},
		{geminiComment{}, ""},
	} {
		if got := findingLabel(tc.comment); got != tc.want {
			t.Errorf("findingLabel(%+v) = %q, want %q", tc.comment, got, tc.want)
		}
	}
}

// The gate tells a thread's finding from the label post puts on its line
// comment, so the two must agree.
func TestTheGateReadsTheIDFromEveryLabelPostWrites(t *testing.T) {
	for _, comment := range []geminiComment{
		{Severity: "critical", ID: "F12", Body: "Leaks the token."},
		{Severity: "concern", ID: "F1", Body: "May race."},
		{ID: "F4", Body: "**[STANDARDS]** `config-in-app`: reads the environment."},
	} {
		match := labelledIDPattern.FindStringSubmatch(withSeverity(comment))
		if match == nil || match[1] != comment.ID {
			t.Errorf("read %v from %q, want %s", match, withSeverity(comment), comment.ID)
		}
	}
	if match := labelledIDPattern.FindStringSubmatch("Thanks, **F2** is fixed."); match != nil {
		t.Errorf("read %v from a reply that only mentions a finding", match)
	}
}

func TestFindingsMarkerRoundTrips(t *testing.T) {
	comments := []geminiComment{
		{ID: "F1", Severity: "critical", Path: "internal/oura/client.go", Line: 40, Body: "**Leaks** the token.\nMore detail."},
		{ID: "F2", Path: "a.go", Line: 1, Body: "Ends the marker early? -->"},
	}
	body := "Review text.\n\n" + findingsMarker(comments)

	records, ok, err := readFindingsMarker(body)

	if err != nil || !ok {
		t.Fatalf("readFindingsMarker: ok %t, %v", ok, err)
	}
	want := []findingRecord{
		{ID: "F1", Severity: "critical", Path: "internal/oura/client.go", Line: 40, Summary: "Leaks the token."},
		{ID: "F2", Path: "a.go", Line: 1, Summary: "Ends the marker early? -->"},
	}
	if len(records) != len(want) || records[0] != want[0] || records[1] != want[1] {
		t.Errorf("records = %+v, want %+v", records, want)
	}
	if strings.Count(body, "-->") != 1 {
		t.Errorf("a finding's text closed the marker early:\n%s", body)
	}
}

func TestReadFindingsMarkerTellsAMissingRecordFromABrokenOne(t *testing.T) {
	if _, ok, err := readFindingsMarker("A review from before the gate."); ok || err != nil {
		t.Errorf("no marker: ok %t, err %v; want neither", ok, err)
	}
	for name, body := range map[string]string{
		"unclosed": findingsMarkerStart + `{"findings": []}`,
		"not JSON": findingsMarkerStart + `findings` + findingsMarkerEnd,
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := readFindingsMarker(body); err == nil {
				t.Error("readFindingsMarker accepted it")
			}
		})
	}
}

func TestFirstLineSkipsBlankLinesAndCutsLongOnes(t *testing.T) {
	if got := firstLine("\n\n  **Bold** start.\nSecond.", 50); got != "Bold start." {
		t.Errorf("firstLine = %q", got)
	}
	if got := firstLine("ééééé", 3); got != "ééé…" {
		t.Errorf("firstLine = %q, want three runes and an ellipsis", got)
	}
	if got := firstLine("  \n ", 3); got != "" {
		t.Errorf("firstLine of blank text = %q", got)
	}
}

func TestPostedReviewRecordsItsFindingsForTheGate(t *testing.T) {
	review := assembledReview{Summary: "One.", Comments: []geminiComment{
		{ID: "F1", Path: "a.go", Line: 3, Severity: "high", Body: "On the diff."},
		{ID: "F2", Path: "a.go", Line: 40, Severity: "critical", Body: "Off the diff."},
	}}

	request := buildReview(review, "0123456789", "", map[anchor]bool{{path: "a.go", side: sideRight, line: 3}: true})

	if len(request.Comments) != 1 || request.Comments[0].Body != "**[HIGH] F1** On the diff." {
		t.Errorf("line comments = %+v", request.Comments)
	}
	if !strings.Contains(request.Body, "`a.go` line 40: **[CRITICAL] F2** Off the diff.") {
		t.Errorf("body lacks the finding outside the diff:\n%s", request.Body)
	}
	records, ok, err := readFindingsMarker(request.Body)
	if err != nil || !ok || len(records) != 2 || records[1].ID != "F2" || records[1].Severity != "critical" {
		t.Errorf("records = %+v, ok %t, err %v", records, ok, err)
	}
}
