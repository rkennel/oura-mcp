package main

import (
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"
)

// postedReview is the Gemini review of the pull request's head commit, as
// the gate reads it back from GitHub.
type postedReview struct {
	review   githubReview
	findings []findingRecord
	threads  map[int64]string  // the review's line comment → its finding's ID
	links    map[string]string // a finding's ID → its line comment's address
}

// known reports whether the review has a finding with this ID.
func (p postedReview) known(id string) bool {
	for _, f := range p.findings {
		if f.ID == id {
			return true
		}
	}
	return false
}

// link is where a finding can be read: its line comment, or the review for
// a finding that could not go on a line.
func (p postedReview) link(id string) string {
	if url, ok := p.links[id]; ok {
		return url
	}
	return p.review.HTMLURL
}

// threadOf says which finding a comment can dismiss. A reply in a
// finding's thread is about that finding. A comment that starts no thread
// of this review, in the conversation or on a line, may name one, but only
// once the review exists: an earlier comment answered an earlier review.
// about is false for the review's own comments, replies in other threads
// and earlier comments.
func (p postedReview) threadOf(c githubComment) (findingID string, about bool) {
	if c.InReplyToID != 0 {
		findingID, about = p.threads[c.InReplyToID]
		return findingID, about
	}
	if _, own := p.threads[c.ID]; own {
		return "", false
	}
	return "", !c.CreatedAt.Before(p.review.SubmittedAt)
}

// dismissal is the repository owner's reason for letting a finding through.
type dismissal struct {
	author, reason, url string
}

type dismissedFinding struct {
	finding findingRecord
	by      dismissal
}

// ignoredDismissal is a dismissal the gate did not apply, and why, so the
// summary can say why a finding is still open.
type ignoredDismissal struct {
	author, url, why string
}

// gateResult is the gate's verdict on the head commit.
type gateResult struct {
	head      string
	threshold string
	review    *postedReview // nil when there is no review of the head commit
	open      []findingRecord
	dismissed []dismissedFinding
	ignored   []ignoredDismissal
}

// evaluateGate decides whether the review of head holds the merge. A
// confirmed finding at threshold or above holds it unless owner dismissed
// it with a reason; a later dismissal of the same finding replaces an
// earlier one. Without a review, nothing is known, and the merge is held.
func evaluateGate(head, threshold, owner string, review *postedReview, comments []githubComment) gateResult {
	result := gateResult{head: head, threshold: threshold, review: review}
	if review == nil {
		return result
	}
	dismissals := map[string]dismissal{}
	for _, c := range byCreation(comments) {
		thread, about := review.threadOf(c)
		if !about {
			continue
		}
		named, reason, isDismissal := parseDismissal(c.Body)
		if !isDismissal {
			continue
		}
		target, why := dismissalTarget(*review, owner, c.User.Login, thread, named, reason)
		if why != "" {
			result.ignored = append(result.ignored, ignoredDismissal{author: c.User.Login, url: c.HTMLURL, why: why})
			continue
		}
		dismissals[target] = dismissal{author: c.User.Login, reason: reason, url: c.HTMLURL}
	}
	for _, f := range review.findings {
		if d, ok := dismissals[f.ID]; ok {
			result.dismissed = append(result.dismissed, dismissedFinding{finding: f, by: d})
			continue
		}
		if f.blocks(threshold) {
			result.open = append(result.open, f)
		}
	}
	return result
}

// dismissalTarget names the finding a dismissal applies to, or says why it
// applies to none.
func dismissalTarget(review postedReview, owner, author, thread, named, reason string) (target, why string) {
	switch {
	case !strings.EqualFold(author, owner):
		return "", fmt.Sprintf("only the repository owner, %s, may dismiss a finding", owner)
	case thread == "" && named == "":
		return "", "it names no finding: reply on the finding's thread, or name it, as in `/dismiss F2 <reason>`"
	case thread != "" && named != "" && named != thread:
		return "", fmt.Sprintf("it names %s in the thread of %s", named, thread)
	}
	target = thread
	if target == "" {
		target = named
	}
	switch {
	case !review.known(target):
		return "", fmt.Sprintf("the review has no finding %s", target)
	case reason == "":
		return "", fmt.Sprintf("it gives no reason for dismissing %s", target)
	}
	return target, ""
}

// byCreation orders comments oldest first, so a later dismissal wins.
func byCreation(comments []githubComment) []githubComment {
	sorted := append([]githubComment(nil), comments...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].CreatedAt.Before(sorted[j].CreatedAt) })
	return sorted
}

// dismissCommand starts a dismissal.
const dismissCommand = "/dismiss"

// parseDismissal reads a comment of the form "/dismiss [F2] <reason>". The
// ID is optional in a finding's thread; the reason is the rest of the
// comment.
func parseDismissal(body string) (findingID, reason string, ok bool) {
	text := strings.TrimSpace(body)
	if len(text) < len(dismissCommand) || !strings.EqualFold(text[:len(dismissCommand)], dismissCommand) {
		return "", "", false
	}
	rest := text[len(dismissCommand):]
	if rest != "" && rest[0] != ':' && !unicode.IsSpace(rune(rest[0])) {
		return "", "", false // another word, such as /dismissal
	}
	rest = strings.TrimSpace(strings.TrimPrefix(rest, ":"))
	if fields := strings.Fields(rest); len(fields) > 0 {
		candidate := strings.ToUpper(strings.TrimSuffix(fields[0], ":"))
		if findingIDPattern.MatchString(candidate) {
			findingID = candidate
			rest = strings.TrimSpace(rest[len(fields[0]):])
		}
	}
	return findingID, strings.TrimSpace(strings.TrimPrefix(rest, ":")), true
}

// passed reports whether the merge may go ahead.
func (g gateResult) passed() bool {
	return g.review != nil && len(g.open) == 0
}

func (g gateResult) state() string {
	if g.passed() {
		return "success"
	}
	return "failure"
}

// statusDescriptionLength is GitHub's limit on a commit status's
// description.
const statusDescriptionLength = 140

// description is the line the pull request shows beside the status.
func (g gateResult) description() string {
	var text string
	switch {
	case g.review == nil:
		text = fmt.Sprintf("No Gemini review of %s, so its findings are unknown", shortSHA(g.head))
	case len(g.open) > 0:
		ids := make([]string, 0, len(g.open))
		for _, f := range g.open {
			ids = append(ids, f.ID)
		}
		text = fmt.Sprintf("%s at %s: %s", countOf(len(g.open), "open finding"), thresholdPhrase(g.threshold), strings.Join(ids, ", "))
	default:
		text = "No open findings at " + thresholdPhrase(g.threshold)
		if len(g.dismissed) > 0 {
			text += fmt.Sprintf("; %d dismissed", len(g.dismissed))
		}
	}
	return firstLine(text, statusDescriptionLength-1)
}

// summary is the job summary: the open findings, the dismissed ones with
// their reasons, and any dismissal that was not applied.
func (g gateResult) summary() string {
	var b strings.Builder
	if g.review == nil {
		fmt.Fprintf(&b, "### Gemini findings: no review\n\n"+
			"There is no Gemini review of commit %s, so whether it has findings is unknown. "+
			"The review check failed or has not posted its review yet; once a review is posted, this check runs again.\n", shortSHA(g.head))
		return b.String()
	}
	if len(g.open) > 0 {
		fmt.Fprintf(&b, "### Gemini findings: %s at %s\n\n", countOf(len(g.open), "open finding"), thresholdPhrase(g.threshold))
	} else {
		fmt.Fprintf(&b, "### Gemini findings: none open at %s\n\n", thresholdPhrase(g.threshold))
	}
	fmt.Fprintf(&b, "From the [review of %s](%s).\n\n", shortSHA(g.head), g.review.review.HTMLURL)
	if len(g.open) > 0 {
		b.WriteString("| Finding | Severity | Where | What |\n|---|---|---|---|\n")
		for _, f := range g.open {
			fmt.Fprintf(&b, "| [%s](%s) | %s | %s | %s |\n", f.ID, g.review.link(f.ID), f.Severity, where(f), tableCell(f.Summary))
		}
		b.WriteString("\nFix them and push, or have the repository owner dismiss one with a reason: see the README.\n\n")
	}
	if len(g.dismissed) > 0 {
		b.WriteString("#### Dismissed\n\n| Finding | Severity | Where | Reason | By |\n|---|---|---|---|---|\n")
		for _, d := range g.dismissed {
			f := d.finding
			fmt.Fprintf(&b, "| [%s](%s) | %s | %s | %s | [%s](%s) |\n", f.ID, g.review.link(f.ID), severityOrNone(f.Severity), where(f), tableCell(d.by.reason), d.by.author, d.by.url)
		}
		b.WriteString("\n")
	}
	if len(g.ignored) > 0 {
		b.WriteString("#### Dismissals not applied\n\n")
		for _, i := range g.ignored {
			fmt.Fprintf(&b, "- [A comment](%s) by %s: %s.\n", i.url, i.author, i.why)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func where(f findingRecord) string {
	return fmt.Sprintf("`%s` line %d", f.Path, f.Line)
}

func severityOrNone(severity string) string {
	if severity == "" {
		return "none"
	}
	return severity
}

// thresholdPhrase says which severities hold a merge.
func thresholdPhrase(threshold string) string {
	if threshold == severityCritical {
		return severityCritical
	}
	return threshold + " or above"
}

func countOf(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// latestReview picks, from the pull request's reviews, the latest one the
// workflow posted on head that records its findings. ok is false when
// there is none.
func latestReview(reviews []githubReview, head string) (review githubReview, findings []findingRecord, ok bool, err error) {
	var latest time.Time
	for _, r := range reviews {
		if r.User.Login != reviewerLogin || r.CommitID != head {
			continue
		}
		records, recorded, err := readFindingsMarker(r.Body)
		if err != nil {
			return githubReview{}, nil, false, fmt.Errorf("review %d: %w", r.ID, err)
		}
		if recorded && (!ok || !r.SubmittedAt.Before(latest)) {
			review, findings, ok, latest = r, records, true, r.SubmittedAt
		}
	}
	return review, findings, ok, nil
}
