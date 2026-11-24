package main

import (
	"fmt"
	"strings"
)

// Verdicts Gemini can give an acceptance criterion.
const (
	criterionSatisfied    = "satisfied"
	criterionPartly       = "partly"
	criterionNotSatisfied = "not_satisfied"
)

// storyReply is Gemini's judgement of the change against the story.
type storyReply struct {
	Criteria []criterionVerdict `json:"criteria"`
	Outside  []outsideChange    `json:"outside"`
}

// criterionVerdict is Gemini's verdict on one acceptance criterion.
type criterionVerdict struct {
	ID       string `json:"id"`
	Verdict  string `json:"verdict"`
	Reason   string `json:"reason"`
	Evidence string `json:"evidence"`
}

// outsideChange is a change of behaviour that no part of the story
// explains.
type outsideChange struct {
	Path   string `json:"path"`
	Line   int    `json:"line"`
	Change string `json:"change"`
}

// criterionRow is one row of the posted criteria: the criterion and
// Gemini's verdict on it.
type criterionRow struct {
	Criterion criterion
	Verdict   criterionVerdict
}

// storyReview is the review of the change against its story. When the
// story is unavailable it holds only the reason.
type storyReview struct {
	Story    story
	Criteria []criterionRow
	Outside  []outsideChange
}

// unmet reports whether a criterion is not, or only partly, satisfied.
func (s storyReview) unmet() bool {
	for _, row := range s.Criteria {
		if row.Verdict.Verdict != criterionSatisfied {
			return true
		}
	}
	return false
}

// verdictFor returns the verdict on the criterion with the ID, or "".
func (s storyReview) verdictFor(id string) string {
	for _, row := range s.Criteria {
		if row.Criterion.ID == id {
			return row.Verdict.Verdict
		}
	}
	return ""
}

// assessStory checks Gemini's story reply against the story: when the
// story has criteria, the reply must give one verdict, with a reason, on
// each of them, and nothing else. When it has none, any reply is ignored.
func assessStory(implemented story, reply *storyReply) (storyReview, error) {
	review := storyReview{Story: implemented}
	if !implemented.available() {
		return review, nil
	}
	if reply == nil {
		return storyReview{}, fmt.Errorf("gemini's review judges none of the acceptance criteria of %s", implemented.Key)
	}
	verdicts, err := indexCriterionVerdicts(reply.Criteria, implemented)
	if err != nil {
		return storyReview{}, err
	}
	var missing []string
	for _, c := range implemented.Criteria {
		verdict, ok := verdicts[c.ID]
		if !ok {
			missing = append(missing, c.ID)
			continue
		}
		review.Criteria = append(review.Criteria, criterionRow{Criterion: c, Verdict: verdict})
	}
	if len(missing) > 0 {
		return storyReview{}, fmt.Errorf("gemini's review omits the acceptance criteria %s", strings.Join(missing, ", "))
	}
	for i, change := range reply.Outside {
		if strings.TrimSpace(change.Path) == "" || strings.TrimSpace(change.Change) == "" {
			return storyReview{}, fmt.Errorf("gemini's change outside the story %d has no path or no description", i+1)
		}
	}
	review.Outside = reply.Outside
	return review, nil
}

func indexCriterionVerdicts(entries []criterionVerdict, implemented story) (map[string]criterionVerdict, error) {
	known := map[string]bool{}
	for _, id := range implemented.criterionIDs() {
		known[id] = true
	}
	index := map[string]criterionVerdict{}
	for i, entry := range entries {
		entry.ID = strings.ToUpper(strings.TrimSpace(entry.ID))
		entry.Verdict = strings.ToLower(strings.TrimSpace(entry.Verdict))
		entry.Reason = strings.TrimSpace(entry.Reason)
		entry.Evidence = strings.TrimSpace(entry.Evidence)
		switch {
		case !known[entry.ID]:
			return nil, fmt.Errorf("gemini's criterion verdict %d names %q, which is not a criterion of %s", i+1, entry.ID, implemented.Key)
		case !knownCriterionVerdict(entry.Verdict):
			return nil, fmt.Errorf("gemini's verdict on %s is %q, not satisfied, partly or not_satisfied", entry.ID, entry.Verdict)
		case entry.Reason == "":
			return nil, fmt.Errorf("gemini's verdict on %s gives no reason", entry.ID)
		case index[entry.ID].ID != "":
			return nil, fmt.Errorf("gemini's review judges %s twice", entry.ID)
		}
		index[entry.ID] = entry
	}
	return index, nil
}

func knownCriterionVerdict(verdict string) bool {
	switch verdict {
	case criterionSatisfied, criterionPartly, criterionNotSatisfied:
		return true
	}
	return false
}

// storySection renders the review against the story for the review's body:
// a verdict on each criterion and the changes outside the story, or why
// there were no criteria to check.
func storySection(review storyReview) string {
	var b strings.Builder
	if !review.Story.available() {
		b.WriteString("### Acceptance criteria\n\n")
		fmt.Fprintf(&b, "Not checked: %s. The review above did not compare the change with a story.\n", review.Story.Unavailable)
		return b.String()
	}
	fmt.Fprintf(&b, "### Acceptance criteria of %s\n\n", review.Story.Key)
	fmt.Fprintf(&b, "%s\n\n", tableCell(review.Story.Summary))
	b.WriteString("| Criterion | Verdict | Reason | Evidence |\n|---|---|---|---|\n")
	for _, row := range review.Criteria {
		fmt.Fprintf(&b, "| **%s** %s | %s | %s | %s |\n", row.Criterion.ID, tableCell(row.Criterion.Text),
			criterionLabel(row.Verdict.Verdict), tableCell(row.Verdict.Reason), tableCell(row.Verdict.Evidence))
	}
	b.WriteString("\n#### Changes outside the story\n\n")
	if len(review.Outside) == 0 {
		b.WriteString("None: every change serves the story.\n")
	}
	for _, change := range review.Outside {
		fmt.Fprintf(&b, "- `%s` line %d: %s\n", change.Path, change.Line, strings.TrimSpace(change.Change))
	}
	return b.String()
}

func criterionLabel(verdict string) string {
	switch verdict {
	case criterionSatisfied:
		return "✅ satisfied"
	case criterionPartly:
		return "🟡 partly satisfied"
	default:
		return "❌ not satisfied"
	}
}
