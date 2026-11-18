package main

import (
	"strings"
	"testing"
)

func TestParseGeminiReviewAcceptsBareAndFencedJSON(t *testing.T) {
	const object = `{"summary": "Looks fine.", "comments": [{"path": "a.go", "line": 3, "side": "RIGHT", "severity": "low", "body": "Typo."}]}`
	for name, reply := range map[string]string{
		"bare":   object,
		"fenced": "```json\n" + object + "\n```",
		"prose":  "Here is the review:\n" + object + "\nDone.",
	} {
		t.Run(name, func(t *testing.T) {
			review, err := parseGeminiReview(reply)
			if err != nil {
				t.Fatalf("parseGeminiReview: %v", err)
			}
			if review.Summary != "Looks fine." || len(review.Comments) != 1 || review.Comments[0].Line != 3 {
				t.Errorf("review = %+v", review)
			}
		})
	}
}

func TestParseGeminiReviewRejectsUnusableReplies(t *testing.T) {
	for name, reply := range map[string]string{
		"empty":            "",
		"no JSON":          "I could not review this pull request.",
		"broken JSON":      `{"summary": "x", "comments": [}`,
		"no summary":       `{"summary": "  ", "comments": []}`,
		"comment no path":  `{"summary": "x", "comments": [{"line": 1, "body": "b"}]}`,
		"comment no body":  `{"summary": "x", "comments": [{"path": "a.go", "line": 1}]}`,
		"wrong field type": `{"summary": 7}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseGeminiReview(reply); err == nil {
				t.Errorf("parseGeminiReview(%q) succeeded", reply)
			}
		})
	}
}

func TestBuildReviewPlacesCommentsOnTheDiffAndMovesTheRestToTheBody(t *testing.T) {
	onDiff := map[anchor]bool{
		{"a.go", sideRight, 3}: true,
		{"a.go", sideLeft, 5}:  true,
	}
	review := geminiReview{
		Summary: "One bug, one nit.",
		Comments: []geminiComment{
			{Path: "a.go", Line: 3, Severity: "high", Body: "Nil dereference."},
			{Path: "a.go", Line: 5, Side: "left", Body: "This check was needed."},
			{Path: "a.go", Line: 40, Side: "RIGHT", Severity: "low", Body: "Outside the diff."},
		},
	}

	got := buildReview(review, "0123456789abcdef", onDiff)

	if got.CommitID != "0123456789abcdef" || got.Event != "COMMENT" {
		t.Errorf("commit and event = %q, %q", got.CommitID, got.Event)
	}
	want := []reviewComment{
		{Path: "a.go", Line: 3, Side: sideRight, Body: "**[HIGH]** Nil dereference."},
		{Path: "a.go", Line: 5, Side: sideLeft, Body: "This check was needed."},
	}
	if len(got.Comments) != len(want) {
		t.Fatalf("comments = %+v, want %+v", got.Comments, want)
	}
	for i := range want {
		if got.Comments[i] != want[i] {
			t.Errorf("comment %d = %+v, want %+v", i, got.Comments[i], want[i])
		}
	}
	for _, part := range []string{
		"One bug, one nit.",
		"### Comments outside the diff",
		"- `a.go` line 40: **[LOW]** Outside the diff.",
		"Gemini CLI review of 0123456.",
	} {
		if !strings.Contains(got.Body, part) {
			t.Errorf("body lacks %q:\n%s", part, got.Body)
		}
	}
	if strings.Contains(got.Body, noFindings) {
		t.Errorf("body of a review with comments says no issues were found:\n%s", got.Body)
	}
}

func TestBuildReviewWithNoCommentsSaysNothingWasFound(t *testing.T) {
	got := buildReview(geminiReview{Summary: "A clean refactor."}, "abc", nil)

	if len(got.Comments) != 0 {
		t.Errorf("comments = %+v, want none", got.Comments)
	}
	if !strings.HasPrefix(got.Body, noFindings) || !strings.Contains(got.Body, "A clean refactor.") {
		t.Errorf("body = %q, want the no-findings note then the summary", got.Body)
	}
	if strings.Contains(got.Body, "Comments outside the diff") {
		t.Errorf("body lists comments outside the diff when there are none:\n%s", got.Body)
	}
}

func TestWithoutLineCommentsPutsEveryCommentInTheBody(t *testing.T) {
	review := geminiReview{
		Summary:  "Two issues.",
		Comments: []geminiComment{{Path: "a.go", Line: 1, Body: "First."}, {Path: "b.go", Line: 2, Body: "Second."}},
	}

	got := withoutLineComments(review, "abc")

	if len(got.Comments) != 0 || got.Event != "COMMENT" || got.CommitID != "abc" {
		t.Errorf("review = %+v, want a COMMENT review on abc with no line comments", got)
	}
	for _, part := range []string{"- `a.go` line 1: First.", "- `b.go` line 2: Second."} {
		if !strings.Contains(got.Body, part) {
			t.Errorf("body lacks %q:\n%s", part, got.Body)
		}
	}
}
