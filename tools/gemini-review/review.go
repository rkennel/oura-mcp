package main

import (
	"errors"
	"fmt"
	"strings"
)

// geminiReview is the JSON object the review prompt asks Gemini to return.
type geminiReview struct {
	Summary   string           `json:"summary"`
	Comments  []geminiComment  `json:"comments"`
	Checklist []checklistEntry `json:"checklist"`
}

type geminiComment struct {
	Path     string `json:"path"`
	Line     int    `json:"line"`
	Side     string `json:"side"`
	Severity string `json:"severity"`
	Body     string `json:"body"`
}

// reviewRequest is the body of GitHub's "create a review" call.
type reviewRequest struct {
	CommitID string          `json:"commit_id"`
	Event    string          `json:"event"`
	Body     string          `json:"body"`
	Comments []reviewComment `json:"comments,omitempty"`
}

type reviewComment struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Side string `json:"side"`
	Body string `json:"body"`
}

const noFindings = "**No issues found.** Neither Gemini nor the standards check has comments on this change."

// onlyConcerns opens a review whose only findings are concerns, so that a
// doubt Gemini could not prove is not read as a confirmed problem.
const onlyConcerns = "**No confirmed issues.** Gemini raised concerns it could not prove, marked CONCERN; weigh them before merging."

// Severities a comment can carry. A concern is a problem Gemini finds
// plausible but cannot prove; the others are confirmed, from most to least
// severe.
const (
	severityCritical = "critical"
	severityHigh     = "high"
	severityMedium   = "medium"
	severityLow      = "low"
	severityConcern  = "concern"
)

// severityRank orders the severities, so that a seeded case can ask for a
// comment of at least some severity. A concern ranks below every confirmed
// severity, and a missing or unknown one below that.
var severityRank = map[string]int{
	severityConcern:  1,
	severityLow:      2,
	severityMedium:   3,
	severityHigh:     4,
	severityCritical: 5,
}

// concernNote follows the label of a concern, so that it reads as a doubt
// to weigh rather than a defect to fix.
const concernNote = "*Not confirmed: Gemini could not prove this from the diff and the repository.*"

// parseGeminiReview extracts the review from Gemini's reply. The reply should
// be the bare JSON object, but a model may wrap it in a code fence or a
// sentence, so the outermost braces are taken.
func parseGeminiReview(reply string) (geminiReview, error) {
	var review geminiReview
	if err := unmarshalOutermostObject(reply, &review); err != nil {
		return geminiReview{}, fmt.Errorf("gemini's review: %w", err)
	}
	if strings.TrimSpace(review.Summary) == "" {
		return geminiReview{}, errors.New("gemini's review has no summary")
	}
	for i, comment := range review.Comments {
		if strings.TrimSpace(comment.Path) == "" || strings.TrimSpace(comment.Body) == "" {
			return geminiReview{}, fmt.Errorf("gemini's comment %d has no path or no body", i+1)
		}
		severity := strings.ToLower(strings.TrimSpace(comment.Severity))
		if _, known := severityRank[severity]; severity != "" && !known {
			return geminiReview{}, fmt.Errorf("gemini's comment %d has severity %q, not critical, high, medium, low or concern", i+1, comment.Severity)
		}
		review.Comments[i].Severity = severity
	}
	return review, nil
}

// isConcern reports whether the comment is a doubt rather than a confirmed
// problem.
func (c geminiComment) isConcern() bool {
	return c.Severity == severityConcern
}

// atLeast reports whether the comment is confirmed and at least as severe
// as severity.
func (c geminiComment) atLeast(severity string) bool {
	return !c.isConcern() && severityRank[c.Severity] >= severityRank[severity]
}

// buildReview turns the review into a GitHub review on commitID. A
// comment whose line is not on the diff cannot be posted as a line comment,
// so it moves into the review's body instead of being dropped. modelNote says
// which model reviewed and why.
func buildReview(review assembledReview, commitID, modelNote string, onDiff map[anchor]bool) reviewRequest {
	request := reviewRequest{CommitID: commitID, Event: "COMMENT"}
	var unplaced []geminiComment
	for _, comment := range review.Comments {
		side := strings.ToUpper(strings.TrimSpace(comment.Side))
		if side == "" {
			side = sideRight
		}
		if !onDiff[anchor{path: comment.Path, side: side, line: comment.Line}] {
			unplaced = append(unplaced, comment)
			continue
		}
		request.Comments = append(request.Comments, reviewComment{
			Path: comment.Path,
			Line: comment.Line,
			Side: side,
			Body: withSeverity(comment),
		})
	}
	request.Body = reviewBody(review, unplaced, commitID, modelNote)
	return request
}

// withoutLineComments is the fallback when GitHub rejects the line comments:
// the same review with every comment in its body.
func withoutLineComments(review assembledReview, commitID, modelNote string) reviewRequest {
	return reviewRequest{
		CommitID: commitID,
		Event:    "COMMENT",
		Body:     reviewBody(review, review.Comments, commitID, modelNote),
	}
}

// reviewBody is Gemini's summary, then any comments that could not go on a
// line, then the verdict on every rule, then which model reviewed and why. A
// review that found nothing says so, so a quiet review cannot be mistaken
// for one that never ran, and one that found only concerns says that none
// is confirmed.
func reviewBody(review assembledReview, unplaced []geminiComment, commitID, modelNote string) string {
	var b strings.Builder
	switch {
	case review.hasFindings():
	case review.hasConcerns():
		b.WriteString(onlyConcerns + "\n\n")
	default:
		b.WriteString(noFindings + "\n\n")
	}
	b.WriteString(strings.TrimSpace(review.Summary))
	b.WriteString("\n\n")
	if len(unplaced) > 0 {
		b.WriteString("### Comments outside the diff\n\n")
		for _, comment := range unplaced {
			fmt.Fprintf(&b, "- `%s` line %d: %s\n", comment.Path, comment.Line, withSeverity(comment))
		}
		b.WriteString("\n")
	}
	if len(review.Checklist) > 0 {
		b.WriteString(checklistTable(review.Checklist) + "\n")
	}
	if modelNote != "" {
		b.WriteString(modelNote + "\n\n")
	}
	fmt.Fprintf(&b, "<sub>Gemini CLI review of %s.</sub>", shortSHA(commitID))
	return b.String()
}

// withSeverity labels the comment's body with its severity, and marks a
// concern as unconfirmed.
func withSeverity(comment geminiComment) string {
	severity := strings.ToUpper(strings.TrimSpace(comment.Severity))
	body := strings.TrimSpace(comment.Body)
	switch {
	case severity == "":
		return body
	case comment.isConcern():
		return fmt.Sprintf("**[%s]** %s %s", severity, concernNote, body)
	default:
		return fmt.Sprintf("**[%s]** %s", severity, body)
	}
}

func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}
