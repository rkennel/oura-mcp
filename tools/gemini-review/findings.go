package main

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// findingRecord is what the gate needs to know about one finding of a
// posted review. The review carries its findings in a hidden marker, so the
// gate reads them back without parsing the Markdown around them.
type findingRecord struct {
	ID       string `json:"id"`
	Severity string `json:"severity"`
	Path     string `json:"path"`
	Line     int    `json:"line"`
	Summary  string `json:"summary"`
}

// blocks reports whether the finding holds a merge at threshold: it is
// confirmed, not a concern, and at least that severe.
func (f findingRecord) blocks(threshold string) bool {
	return geminiComment{Severity: f.Severity}.atLeast(threshold)
}

// The marker is an HTML comment, which GitHub does not render. The JSON in
// it is encoded with HTML escaping, so a finding's text cannot close it.
const (
	findingsMarkerStart = "<!-- gemini-review-findings "
	findingsMarkerEnd   = " -->"
)

// findingSummaryLength bounds a finding's summary in the marker, which the
// gate shows in a table cell.
const findingSummaryLength = 200

// findingIDPattern matches a finding's ID, as the review labels it and a
// dismissal names it.
var findingIDPattern = regexp.MustCompile(`^F[1-9][0-9]*$`)

// labelledIDPattern finds the ID in a line comment's label, such as
// "**[HIGH] F2**", which is how the gate tells which finding a thread is
// about.
var labelledIDPattern = regexp.MustCompile(`^\*\*(?:\[[A-Z]+\] )?(F[1-9][0-9]*)\*\*`)

// numberFindings gives each comment its ID, F1 onwards, in the order the
// review lists them.
func numberFindings(comments []geminiComment) {
	for i := range comments {
		comments[i].ID = fmt.Sprintf("F%d", i+1)
	}
}

// findingsMarker records the comments for the gate.
func findingsMarker(comments []geminiComment) string {
	records := make([]findingRecord, 0, len(comments))
	for _, comment := range comments {
		records = append(records, findingRecord{
			ID: comment.ID, Severity: comment.Severity, Path: comment.Path, Line: comment.Line,
			Summary: firstLine(comment.Body, findingSummaryLength),
		})
	}
	// Marshalling plain structs of strings and ints cannot fail.
	raw, _ := json.Marshal(struct {
		Findings []findingRecord `json:"findings"`
	}{records})
	return findingsMarkerStart + string(raw) + findingsMarkerEnd
}

// readFindingsMarker reads the findings a review body records. ok is false
// when the body has no marker, as a review posted before the gate existed.
func readFindingsMarker(body string) (records []findingRecord, ok bool, err error) {
	start := strings.Index(body, findingsMarkerStart)
	if start < 0 {
		return nil, false, nil
	}
	rest := body[start+len(findingsMarkerStart):]
	end := strings.Index(rest, findingsMarkerEnd)
	if end < 0 {
		return nil, false, fmt.Errorf("the review's findings record is not closed")
	}
	var parsed struct {
		Findings []findingRecord `json:"findings"`
	}
	if err := json.Unmarshal([]byte(rest[:end]), &parsed); err != nil {
		return nil, false, fmt.Errorf("the review's findings record: %w", err)
	}
	return parsed.Findings, true, nil
}

// firstLine is the first non-empty line of text, without Markdown emphasis
// markers, cut to max runes.
func firstLine(text string, max int) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(strings.ReplaceAll(line, "**", ""))
		if line == "" {
			continue
		}
		if utf8.RuneCountInString(line) > max {
			return string([]rune(line)[:max]) + "…"
		}
		return line
	}
	return ""
}

// findingLabel is the bold label that opens a finding: its severity and
// its ID, such as "**[HIGH] F2**".
func findingLabel(comment geminiComment) string {
	var parts []string
	if severity := strings.ToUpper(strings.TrimSpace(comment.Severity)); severity != "" {
		parts = append(parts, "["+severity+"]")
	}
	if comment.ID != "" {
		parts = append(parts, comment.ID)
	}
	if len(parts) == 0 {
		return ""
	}
	return "**" + strings.Join(parts, " ") + "**"
}
