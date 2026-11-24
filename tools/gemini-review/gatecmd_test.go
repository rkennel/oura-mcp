package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// gateGitHub serves the calls the gate makes, a page at a time, and
// records the statuses it sets.
type gateGitHub struct {
	mu             sync.Mutex
	reviews        []githubReview
	reviewComments []githubComment // the line comments of review 1
	lines          []githubComment
	conversation   []githubComment
	statuses       []commitStatus
	statusSHAs     []string
	statusFails    int // when set, answer the status call with this status
}

func (f *gateGitHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer test-token" {
		http.Error(w, "bad credentials", http.StatusUnauthorized)
		return
	}
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/repos/o/r/pulls/7":
		fmt.Fprintf(w, `{"number": 7, "head": {"sha": %q}}`, testHead)
	case r.Method == http.MethodGet && r.URL.Path == "/repos/o/r/pulls/7/reviews":
		servePage(w, r, f.reviews)
	case r.Method == http.MethodGet && r.URL.Path == "/repos/o/r/pulls/7/reviews/1/comments":
		servePage(w, r, f.reviewComments)
	case r.Method == http.MethodGet && r.URL.Path == "/repos/o/r/pulls/7/comments":
		servePage(w, r, f.lines)
	case r.Method == http.MethodGet && r.URL.Path == "/repos/o/r/issues/7/comments":
		servePage(w, r, f.conversation)
	case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/repos/o/r/statuses/"):
		f.recordStatus(w, r)
	default:
		http.NotFound(w, r)
	}
}

// servePage answers one page of a list, as GitHub pages it.
func servePage[T any](w http.ResponseWriter, r *http.Request, items []T) {
	size, err := strconv.Atoi(r.URL.Query().Get("per_page"))
	if err != nil || size <= 0 {
		http.Error(w, "no per_page", http.StatusBadRequest)
		return
	}
	page, err := strconv.Atoi(r.URL.Query().Get("page"))
	if err != nil || page < 1 {
		http.Error(w, "no page", http.StatusBadRequest)
		return
	}
	start := min((page-1)*size, len(items))
	_ = json.NewEncoder(w).Encode(append([]T{}, items[start:min(start+size, len(items))]...))
}

func (f *gateGitHub) recordStatus(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.statusFails != 0 {
		http.Error(w, "refused", f.statusFails)
		return
	}
	var status commitStatus
	if err := json.NewDecoder(r.Body).Decode(&status); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	f.statuses = append(f.statuses, status)
	f.statusSHAs = append(f.statusSHAs, strings.TrimPrefix(r.URL.Path, "/repos/o/r/statuses/"))
	_, _ = w.Write([]byte(`{}`))
}

// runGate runs the gate against fake and returns its log and job summary.
func runGate(t *testing.T, fake *gateGitHub, extra map[string]string) (log, summary string, err error) {
	t.Helper()
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)
	env := map[string]string{
		"ROUTING_CONFIG":      writeFile(t, testRouting),
		"GITHUB_STEP_SUMMARY": filepath.Join(t.TempDir(), "summary.md"),
	}
	for k, v := range extra {
		env[k] = v
	}
	var out bytes.Buffer
	err = run(t.Context(), []string{"gate"}, testEnv(server.URL, "", env), server.Client(), &out)
	return out.String(), readOptional(t, env["GITHUB_STEP_SUMMARY"]), err
}

// reviewedWithACriticalFinding is a pull request whose head commit's review
// found F1, critical, on a line.
func reviewedWithACriticalFinding() *gateGitHub {
	body := "Review.\n" + findingsMarker([]geminiComment{
		{ID: "F1", Severity: severityCritical, Path: "internal/oura/client.go", Line: 40, Body: "Logs the access token."},
		{ID: "F2", Severity: severityLow, Path: "a.go", Line: 2, Body: "Name."},
	})
	return &gateGitHub{
		reviews: []githubReview{{ID: 1, User: githubUser{Login: reviewerLogin}, CommitID: testHead, Body: body,
			HTMLURL: "https://github.com/o/r/pull/7#pullrequestreview-1", SubmittedAt: at(reviewPosts)}},
		reviewComments: []githubComment{
			{ID: threadOfF1, Body: "**[CRITICAL] F1** Logs the access token.", HTMLURL: "https://github.com/o/r/pull/7#discussion_r101"},
		},
	}
}

func TestGateCommandHoldsTheHeadCommitOnAnOpenFinding(t *testing.T) {
	fake := reviewedWithACriticalFinding()

	log, summary, err := runGate(t, fake, map[string]string{
		"GITHUB_SERVER_URL": "https://github.com/", "GITHUB_RUN_ID": "42",
	})

	if err != nil {
		t.Fatalf("gate: %v", err)
	}
	want := commitStatus{State: "failure", Description: "1 open finding at high or above: F1", Context: "Gemini findings",
		TargetURL: "https://github.com/o/r/actions/runs/42"}
	if len(fake.statuses) != 1 || fake.statuses[0] != want || fake.statusSHAs[0] != testHead {
		t.Errorf("statuses = %+v on %v, want %+v on the head", fake.statuses, fake.statusSHAs, want)
	}
	if !strings.Contains(log, "to failure") {
		t.Errorf("log = %q", log)
	}
	if !strings.Contains(summary, "| [F1](https://github.com/o/r/pull/7#discussion_r101) | critical |") {
		t.Errorf("summary = %q", summary)
	}
}

func TestGateCommandPassesOnceTheOwnerRepliesWithAReason(t *testing.T) {
	fake := reviewedWithACriticalFinding()
	fake.lines = append(fake.reviewComments, reply("o", "/dismiss The token in that log line is the fake's.", 20))

	_, summary, err := runGate(t, fake, nil)

	if err != nil {
		t.Fatalf("gate: %v", err)
	}
	if len(fake.statuses) != 1 || fake.statuses[0].State != "success" || fake.statuses[0].TargetURL != "" {
		t.Errorf("statuses = %+v, want success with no run to link outside Actions", fake.statuses)
	}
	if !strings.Contains(summary, "| The token in that log line is the fake's. | [o]") {
		t.Errorf("summary lacks the dismissal:\n%s", summary)
	}
}

func TestGateCommandReadsEveryPageOfComments(t *testing.T) {
	fake := reviewedWithACriticalFinding()
	for i := 0; i < pageSize+5; i++ {
		fake.conversation = append(fake.conversation, conversation("someone", "Looks good.", 11))
	}
	fake.conversation = append(fake.conversation, conversation("o", "/dismiss F1 Accepted for now.", 30))

	if _, _, err := runGate(t, fake, nil); err != nil {
		t.Fatalf("gate: %v", err)
	}
	if len(fake.statuses) != 1 || fake.statuses[0].State != "success" {
		t.Errorf("statuses = %+v, want the dismissal on the second page to count", fake.statuses)
	}
}

func TestGateCommandReportsAMissingReview(t *testing.T) {
	fake := &gateGitHub{}

	_, summary, err := runGate(t, fake, nil)

	if err != nil {
		t.Fatalf("gate: %v", err)
	}
	if len(fake.statuses) != 1 || fake.statuses[0].State != "failure" ||
		fake.statuses[0].Description != "No Gemini review of 0123456, so its findings are unknown" {
		t.Errorf("statuses = %+v", fake.statuses)
	}
	if !strings.Contains(summary, "no review") {
		t.Errorf("summary = %q", summary)
	}
}

func TestGateCommandFailsWhenItCannotSetTheStatus(t *testing.T) {
	fake := reviewedWithACriticalFinding()
	fake.statusFails = http.StatusForbidden

	if _, _, err := runGate(t, fake, nil); err == nil || !strings.Contains(err.Error(), "status 403") {
		t.Errorf("err = %v, want the 403", err)
	}
}

func TestGateCommandFailsOnABadThreshold(t *testing.T) {
	routing := writeFile(t, strings.Replace(testRouting, `"blocking_severity": "high"`, `"blocking_severity": "severe"`, 1))

	_, _, err := runGate(t, reviewedWithACriticalFinding(), map[string]string{"ROUTING_CONFIG": routing})

	if err == nil || !strings.Contains(err.Error(), "blocking_severity") {
		t.Errorf("err = %v, want the bad setting named", err)
	}
}

func TestGateCommandFailsWhenGitHubRefuses(t *testing.T) {
	_, _, err := runGate(t, reviewedWithACriticalFinding(), map[string]string{"GITHUB_TOKEN": "wrong"})

	if err == nil || !strings.Contains(err.Error(), "status 401") {
		t.Errorf("err = %v, want the 401", err)
	}
}

func TestRunURLNeedsAllOfActionsSettings(t *testing.T) {
	env := map[string]string{"GITHUB_SERVER_URL": "https://github.com", "GITHUB_REPOSITORY": "o/r", "GITHUB_RUN_ID": "9"}
	if got := runURL(func(k string) string { return env[k] }); got != "https://github.com/o/r/actions/runs/9" {
		t.Errorf("runURL = %q", got)
	}
	delete(env, "GITHUB_RUN_ID")
	if got := runURL(func(k string) string { return env[k] }); got != "" {
		t.Errorf("runURL without a run = %q", got)
	}
}
