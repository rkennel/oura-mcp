package main

import (
	"fmt"
	"sort"
	"strings"
)

// Verdicts a checklist entry can give a rule. A concern means the change
// may break the rule but Gemini cannot prove it; it is not a failure.
const (
	verdictPass          = "pass"
	verdictFail          = "fail"
	verdictConcern       = "concern"
	verdictNotApplicable = "not_applicable"
)

// checklistEntry is Gemini's verdict on one rule of GEMINI.md.
type checklistEntry struct {
	Rule    string `json:"rule"`
	Verdict string `json:"verdict"`
	Reason  string `json:"reason"`
}

// verdictRow is one row of the posted checklist: a rule, its verdict and
// why, from Gemini or, for a mechanical rule, from the standards check.
type verdictRow struct {
	Rule    rule
	Verdict string
	Reason  string
}

// assembledReview is the whole review: Gemini's summary and comments, the
// standards check's findings as comments, and a verdict on every rule.
type assembledReview struct {
	Summary   string
	Comments  []geminiComment
	Checklist []verdictRow
	Story     storyReview
}

// hasFindings reports whether the review found anything to fix: a
// confirmed comment, a failed rule or an acceptance criterion not fully
// met. Concerns, and changes outside the story, do not count.
func (r assembledReview) hasFindings() bool {
	for _, comment := range r.Comments {
		if !comment.isConcern() {
			return true
		}
	}
	return len(rulesWithVerdict(r.Checklist, verdictFail)) > 0 || r.Story.unmet()
}

// hasConcerns reports whether the review raised a doubt it could not prove,
// as a comment or as a rule's verdict.
func (r assembledReview) hasConcerns() bool {
	for _, comment := range r.Comments {
		if comment.isConcern() {
			return true
		}
	}
	return len(rulesWithVerdict(r.Checklist, verdictConcern)) > 0
}

// verdictFor returns the review's verdict on a rule, or "" if it has none.
func (r assembledReview) verdictFor(ruleID string) verdictRow {
	for _, row := range r.Checklist {
		if row.Rule.ID == ruleID {
			return row
		}
	}
	return verdictRow{}
}

// assembleReview joins Gemini's review and the standards check's result. It
// fails when Gemini's checklist omits a rule it must judge, names a rule
// GEMINI.md does not have, or gives a verdict that is not pass, fail,
// concern or not_applicable, and when the standards check did not cover a
// rule GEMINI.md marks as checked by it. A failure means nothing is posted.
func assembleReview(reply geminiReview, rules []rule, standards standardsResult) (assembledReview, error) {
	if err := checkScriptCoverage(rules, standards); err != nil {
		return assembledReview{}, err
	}
	geminiVerdicts, err := indexChecklist(reply.Checklist, rules)
	if err != nil {
		return assembledReview{}, err
	}

	review := assembledReview{Summary: reply.Summary, Comments: append([]geminiComment(nil), reply.Comments...)}
	var missing []string
	for _, r := range rules {
		if r.Script {
			review.Checklist = append(review.Checklist, scriptVerdict(r, standards.Findings))
			continue
		}
		entry, ok := geminiVerdicts[r.ID]
		if !ok {
			missing = append(missing, r.ID)
			continue
		}
		review.Checklist = append(review.Checklist, verdictRow{Rule: r, Verdict: entry.Verdict, Reason: entry.Reason})
	}
	if len(missing) > 0 {
		return assembledReview{}, fmt.Errorf("gemini's checklist omits the rules %s", strings.Join(missing, ", "))
	}
	for _, f := range standards.Findings {
		review.Comments = append(review.Comments, findingComment(f))
	}
	return review, nil
}

// checkScriptCoverage makes sure the standards check covered every rule
// GEMINI.md says it checks, so that no rule goes without a verdict.
func checkScriptCoverage(rules []rule, standards standardsResult) error {
	covered := map[string]bool{}
	for _, id := range standards.Rules {
		covered[id] = true
	}
	var uncovered []string
	for _, id := range ruleIDs(rules, true) {
		if !covered[id] {
			uncovered = append(uncovered, id)
		}
	}
	if len(uncovered) > 0 {
		return fmt.Errorf("GEMINI.md marks %s as checked by scripts/check-standards.sh, but the check does not cover them",
			strings.Join(uncovered, ", "))
	}
	return nil
}

// indexChecklist validates Gemini's checklist and indexes it by rule. An
// entry for a mechanical rule is left out: the standards check's verdict
// stands for it.
func indexChecklist(entries []checklistEntry, rules []rule) (map[string]checklistEntry, error) {
	known := map[string]rule{}
	for _, r := range rules {
		known[r.ID] = r
	}
	index := map[string]checklistEntry{}
	for i, entry := range entries {
		entry.Rule = strings.TrimSpace(entry.Rule)
		entry.Verdict = strings.ToLower(strings.TrimSpace(entry.Verdict))
		entry.Reason = strings.TrimSpace(entry.Reason)
		r, ok := known[entry.Rule]
		switch {
		case !ok:
			return nil, fmt.Errorf("gemini's checklist entry %d names %q, which is not a rule in GEMINI.md", i+1, entry.Rule)
		case r.Script:
			continue
		case !knownVerdict(entry.Verdict):
			return nil, fmt.Errorf("gemini's verdict on %s is %q, not pass, fail, concern or not_applicable", entry.Rule, entry.Verdict)
		case entry.Reason == "":
			return nil, fmt.Errorf("gemini's verdict on %s gives no reason", entry.Rule)
		case index[entry.Rule].Rule != "":
			return nil, fmt.Errorf("gemini's checklist judges %s twice", entry.Rule)
		}
		index[entry.Rule] = entry
	}
	return index, nil
}

// knownVerdict reports whether verdict is one Gemini may give a rule.
func knownVerdict(verdict string) bool {
	switch verdict {
	case verdictPass, verdictFail, verdictConcern, verdictNotApplicable:
		return true
	}
	return false
}

// scriptVerdict is the standards check's verdict on one mechanical rule.
func scriptVerdict(r rule, findings []finding) verdictRow {
	var places []string
	for _, f := range findings {
		if f.Rule == r.ID {
			places = append(places, fmt.Sprintf("`%s` line %d", f.Path, f.Line))
		}
	}
	if len(places) == 0 {
		return verdictRow{Rule: r, Verdict: verdictPass, Reason: "scripts/check-standards.sh found no breach."}
	}
	return verdictRow{Rule: r, Verdict: verdictFail,
		Reason: fmt.Sprintf("scripts/check-standards.sh found %d: %s.", len(places), strings.Join(places, ", "))}
}

func findingComment(f finding) geminiComment {
	return geminiComment{
		Path: f.Path, Line: f.Line, Side: f.Side,
		Body: fmt.Sprintf("**[STANDARDS]** `%s`: %s. *Found by scripts/check-standards.sh.*", f.Rule, f.Message),
	}
}

// checklistTable renders the verdicts as a Markdown table.
func checklistTable(rows []verdictRow) string {
	var b strings.Builder
	b.WriteString("### Standards checklist\n\n| Rule | Verdict | Reason |\n|---|---|---|\n")
	for _, row := range rows {
		fmt.Fprintf(&b, "| `%s` %s | %s | %s |\n", row.Rule.ID, tableCell(row.Rule.Title), verdictLabel(row.Verdict), tableCell(row.Reason))
	}
	return b.String()
}

func verdictLabel(verdict string) string {
	switch verdict {
	case verdictPass:
		return "✅ pass"
	case verdictFail:
		return "❌ fail"
	case verdictConcern:
		return "⚠️ concern, not confirmed"
	default:
		return "➖ not applicable"
	}
}

// tableCell keeps text inside one Markdown table cell.
func tableCell(text string) string {
	text = strings.ReplaceAll(text, "|", `\|`)
	return strings.Join(strings.Fields(text), " ")
}

// rulesWithVerdict lists, sorted, the IDs of the rules whose verdict is
// verdict.
func rulesWithVerdict(rows []verdictRow, verdict string) []string {
	var ids []string
	for _, row := range rows {
		if row.Verdict == verdict {
			ids = append(ids, row.Rule.ID)
		}
	}
	sort.Strings(ids)
	return ids
}
