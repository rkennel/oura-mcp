package main

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

const (
	testHead    = "0123456789abcdef"
	testOwner   = "o"
	threadOfF1  = int64(101) // the review's line comment for F1
	reviewPosts = 10         // the minute the review was posted
)

// at is a time on the test day, minutes past ten.
func at(minute int) time.Time {
	return time.Date(2026, 11, 24, 10, minute, 0, 0, time.UTC)
}

// reviewWith is a posted review of testHead with these findings; F1 has a
// thread, and a finding outside the diff has none.
func reviewWith(findings ...findingRecord) *postedReview {
	return &postedReview{
		review:   githubReview{ID: 1, HTMLURL: "https://github.com/o/r/pull/7#pullrequestreview-1", SubmittedAt: at(reviewPosts)},
		findings: findings,
		threads:  map[int64]string{threadOfF1: "F1"},
		links:    map[string]string{"F1": "https://github.com/o/r/pull/7#discussion_r101"},
	}
}

func recordOf(id, severity string) findingRecord {
	return findingRecord{ID: id, Severity: severity, Path: "internal/oura/client.go", Line: 40, Summary: "Logs the access token."}
}

// reply is a comment in F1's thread.
func reply(author, body string, minute int) githubComment {
	return githubComment{ID: int64(200 + minute), InReplyToID: threadOfF1, User: githubUser{Login: author}, Body: body,
		HTMLURL: "https://github.com/o/r/pull/7#discussion_r2", CreatedAt: at(minute)}
}

// conversation is a comment in the pull request's conversation.
func conversation(author, body string, minute int) githubComment {
	return githubComment{ID: int64(300 + minute), User: githubUser{Login: author}, Body: body,
		HTMLURL: "https://github.com/o/r/pull/7#issuecomment-3", CreatedAt: at(minute)}
}

func gate(review *postedReview, comments ...githubComment) gateResult {
	return evaluateGate(testHead, severityHigh, testOwner, review, comments)
}

func TestGateFailsOnAFindingAtTheThresholdAndListsIt(t *testing.T) {
	for _, severity := range []string{severityCritical, severityHigh} {
		t.Run(severity, func(t *testing.T) {
			got := gate(reviewWith(recordOf("F1", severity), recordOf("F2", severityLow)))

			if got.passed() || got.state() != "failure" {
				t.Fatalf("state = %s, want failure", got.state())
			}
			if got.description() != "1 open finding at high or above: F1" {
				t.Errorf("description = %q", got.description())
			}
			summary := got.summary()
			for _, part := range []string{"### Gemini findings: 1 open finding at high or above",
				"| [F1](https://github.com/o/r/pull/7#discussion_r101) | " + severity + " | `internal/oura/client.go` line 40 | Logs the access token. |"} {
				if !strings.Contains(summary, part) {
					t.Errorf("summary lacks %q:\n%s", part, summary)
				}
			}
			if strings.Contains(summary, "| [F2]") {
				t.Errorf("summary lists the low finding as open:\n%s", summary)
			}
		})
	}
}

func TestGatePassesWhenNothingReachesTheThreshold(t *testing.T) {
	for name, findings := range map[string][]findingRecord{
		"no findings":     nil,
		"medium and low":  {recordOf("F1", severityMedium), recordOf("F2", severityLow)},
		"a concern":       {recordOf("F1", severityConcern)},
		"standards check": {recordOf("F1", "")},
	} {
		t.Run(name, func(t *testing.T) {
			got := gate(reviewWith(findings...))

			if !got.passed() || got.state() != "success" {
				t.Errorf("state = %s, want success", got.state())
			}
			if got.description() != "No open findings at high or above" {
				t.Errorf("description = %q", got.description())
			}
		})
	}
}

func TestGateThresholdComesFromTheSetting(t *testing.T) {
	review := reviewWith(recordOf("F1", severityHigh))

	if got := evaluateGate(testHead, severityCritical, testOwner, review, nil); !got.passed() {
		t.Error("a high finding held the merge at a critical threshold")
	}
	if got := evaluateGate(testHead, severityMedium, testOwner, reviewWith(recordOf("F1", severityMedium)), nil); got.passed() {
		t.Error("a medium finding let the merge through at a medium threshold")
	}
	if got := evaluateGate(testHead, severityCritical, testOwner, reviewWith(recordOf("F1", severityCritical)), nil); got.description() != "1 open finding at critical: F1" {
		t.Errorf("description = %q", got.description())
	}
}

func TestGatePassesOnceTheOwnerDismissesWithAReason(t *testing.T) {
	for name, dismissal := range map[string]githubComment{
		"reply in its thread":           reply("o", "/dismiss The token is a fake in the test.", 20),
		"reply naming it":               reply("O", "/DISMISS F1: The token is a fake in the test.", 20),
		"in the conversation naming it": conversation("o", "/dismiss f1 The token is a fake in the test.", 20),
	} {
		t.Run(name, func(t *testing.T) {
			got := gate(reviewWith(recordOf("F1", severityCritical)), dismissal)

			if !got.passed() {
				t.Fatalf("state = %s, want success; ignored %+v", got.state(), got.ignored)
			}
			if got.description() != "No open findings at high or above; 1 dismissed" {
				t.Errorf("description = %q", got.description())
			}
			summary := got.summary()
			for _, part := range []string{"#### Dismissed", "| [F1](https://github.com/o/r/pull/7#discussion_r101) | critical |",
				"| The token is a fake in the test. | [" + dismissal.User.Login + "](" + dismissal.HTMLURL + ") |"} {
				if !strings.Contains(summary, part) {
					t.Errorf("summary lacks %q:\n%s", part, summary)
				}
			}
		})
	}
}

func TestGateKeepsAFindingOpenWhenItsDismissalDoesNotCount(t *testing.T) {
	for name, tc := range map[string]struct {
		comment githubComment
		why     string
	}{
		"no reason":                {reply("o", "/dismiss", 20), "it gives no reason for dismissing F1"},
		"only the ID":              {reply("o", "/dismiss F1:", 20), "it gives no reason for dismissing F1"},
		"not the owner":            {reply("collaborator", "/dismiss Fine by me.", 20), "only the repository owner, o, may dismiss a finding"},
		"another finding's thread": {reply("o", "/dismiss F2 Fine.", 20), "it names F2 in the thread of F1"},
		"no finding named":         {conversation("o", "/dismiss Fine.", 20), "it names no finding"},
		"no such finding":          {conversation("o", "/dismiss F9 Fine.", 20), "the review has no finding F9"},
	} {
		t.Run(name, func(t *testing.T) {
			got := gate(reviewWith(recordOf("F1", severityHigh), recordOf("F2", severityLow)), tc.comment)

			if got.passed() || len(got.open) != 1 || got.open[0].ID != "F1" {
				t.Fatalf("open = %+v, want F1 still open", got.open)
			}
			if len(got.ignored) != 1 || !strings.Contains(got.ignored[0].why, tc.why) {
				t.Errorf("ignored = %+v, want %q", got.ignored, tc.why)
			}
			if summary := got.summary(); !strings.Contains(summary, "#### Dismissals not applied") || !strings.Contains(summary, tc.why) {
				t.Errorf("summary does not say why:\n%s", summary)
			}
		})
	}
}

func TestGateIgnoresCommentsThatAreNotAboutThisReview(t *testing.T) {
	review := reviewWith(recordOf("F1", severityHigh))
	for name, comment := range map[string]githubComment{
		"an earlier review's dismissal": conversation("o", "/dismiss F1 Fine.", reviewPosts-1),
		"another thread":                {ID: 400, InReplyToID: 999, User: githubUser{Login: "o"}, Body: "/dismiss Fine.", CreatedAt: at(20)},
		"the review's own comment":      {ID: threadOfF1, User: githubUser{Login: reviewerLogin}, Body: "/dismiss Fine.", CreatedAt: at(reviewPosts)},
		"not a dismissal":               reply("o", "/dismissal is not a word here", 20),
		"plain discussion":              reply("o", "Agreed, fixing it.", 20),
	} {
		t.Run(name, func(t *testing.T) {
			got := gate(review, comment)

			if got.passed() || len(got.ignored) != 0 || len(got.dismissed) != 0 {
				t.Errorf("got open %+v, ignored %+v, dismissed %+v; want F1 open and nothing else", got.open, got.ignored, got.dismissed)
			}
		})
	}
}

func TestGateTakesTheLatestDismissalOfAFinding(t *testing.T) {
	got := gate(reviewWith(recordOf("F1", severityHigh)),
		reply("o", "/dismiss Second thoughts.", 30),
		reply("o", "/dismiss First reason.", 20))

	if len(got.dismissed) != 1 || got.dismissed[0].by.reason != "Second thoughts." {
		t.Errorf("dismissed = %+v, want the later reason", got.dismissed)
	}
}

func TestGateWithoutAReviewSaysSoRatherThanPassing(t *testing.T) {
	got := gate(nil, conversation("o", "/dismiss F1 Fine.", 20))

	if got.passed() || got.state() != "failure" {
		t.Errorf("state = %s, want failure", got.state())
	}
	if got.description() != "No Gemini review of 0123456, so its findings are unknown" {
		t.Errorf("description = %q", got.description())
	}
	summary := got.summary()
	if !strings.Contains(summary, "### Gemini findings: no review") || strings.Contains(summary, "none open") {
		t.Errorf("summary = %q", summary)
	}
}

func TestGateDescriptionFitsGitHubsLimit(t *testing.T) {
	var findings []findingRecord
	for i := 1; i <= 60; i++ {
		findings = append(findings, recordOf("F"+strings.Repeat("1", i%5+1), severityHigh))
	}

	got := gate(reviewWith(findings...)).description()

	if utf8.RuneCountInString(got) > statusDescriptionLength {
		t.Errorf("description has %d characters, more than %d", utf8.RuneCountInString(got), statusDescriptionLength)
	}
}

func TestParseDismissal(t *testing.T) {
	for _, tc := range []struct {
		body, id, reason string
		ok               bool
	}{
		{"/dismiss Accepted risk.", "", "Accepted risk.", true},
		{"  /dismiss F3 Accepted\nrisk.  ", "F3", "Accepted\nrisk.", true},
		{"/dismiss F3: Accepted risk.", "F3", "Accepted risk.", true},
		{"/dismiss: Accepted risk.", "", "Accepted risk.", true},
		{"/Dismiss f12 Fine", "F12", "Fine", true},
		{"/dismiss First, this is fine.", "", "First, this is fine.", true},
		{"/dismiss F0 Not an ID.", "", "F0 Not an ID.", true},
		{"/dismiss", "", "", true},
		{"/dismissal", "", "", false},
		{"Please /dismiss F1 Fine.", "", "", false},
		{"/dis", "", "", false},
	} {
		id, reason, ok := parseDismissal(tc.body)
		if id != tc.id || reason != tc.reason || ok != tc.ok {
			t.Errorf("parseDismissal(%q) = %q, %q, %t; want %q, %q, %t", tc.body, id, reason, ok, tc.id, tc.reason, tc.ok)
		}
	}
}

func TestLatestReviewTakesTheWorkflowsLastReviewOfTheHead(t *testing.T) {
	marker := func(id string) string {
		return "Review.\n" + findingsMarker([]geminiComment{{ID: id, Severity: severityHigh, Path: "a.go", Line: 1, Body: "x"}})
	}
	bot := githubUser{Login: reviewerLogin}
	reviews := []githubReview{
		{ID: 1, User: bot, CommitID: "earlier", Body: marker("F1"), SubmittedAt: at(1)},
		{ID: 2, User: bot, CommitID: testHead, Body: marker("F2"), SubmittedAt: at(3)},
		{ID: 3, User: bot, CommitID: testHead, Body: marker("F3"), SubmittedAt: at(2)},
		{ID: 4, User: githubUser{Login: "o"}, CommitID: testHead, Body: marker("F4"), SubmittedAt: at(4)},
		{ID: 5, User: bot, CommitID: testHead, Body: "A review from before the gate.", SubmittedAt: at(5)},
	}

	got, findings, ok, err := latestReview(reviews, testHead)

	if err != nil || !ok || got.ID != 2 || len(findings) != 1 || findings[0].ID != "F2" {
		t.Errorf("latestReview = review %d, %+v, ok %t, err %v; want review 2", got.ID, findings, ok, err)
	}
}

// A critical finding on an earlier push does not hold the head commit,
// which the later push fixed.
func TestLatestReviewIgnoresEarlierPushes(t *testing.T) {
	earlier := githubReview{ID: 1, User: githubUser{Login: reviewerLogin}, CommitID: "earlier",
		Body: findingsMarker([]geminiComment{{ID: "F1", Severity: severityCritical}})}

	if _, _, ok, err := latestReview([]githubReview{earlier}, testHead); ok || err != nil {
		t.Errorf("found a review of the head in one of an earlier push: ok %t, err %v", ok, err)
	}
}

func TestLatestReviewFailsOnABrokenRecord(t *testing.T) {
	broken := githubReview{ID: 1, User: githubUser{Login: reviewerLogin}, CommitID: testHead, Body: findingsMarkerStart + "{"}

	if _, _, _, err := latestReview([]githubReview{broken}, testHead); err == nil {
		t.Error("latestReview accepted a review whose findings it cannot read")
	}
}
