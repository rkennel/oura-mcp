package main

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeJira serves issues by key to the test account's token, and answers
// any other key with status.
type fakeJira struct {
	issues map[string]string // key to the issue's JSON
	status int               // the answer for a key it does not have; 404 when unset
	asked  []string
}

func (f *fakeJira) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	user, token, ok := r.BasicAuth()
	if !ok || user != "dev@example.com" || token != "jira-token" {
		http.Error(w, `{"errorMessages": ["secret detail"]}`, http.StatusUnauthorized)
		return
	}
	key, found := strings.CutPrefix(r.URL.Path, "/rest/api/3/issue/")
	if r.Method != http.MethodGet || !found || r.URL.Query().Get("fields") != "summary,description" {
		http.Error(w, "unexpected request", http.StatusBadRequest)
		return
	}
	f.asked = append(f.asked, key)
	issue, ok := f.issues[key]
	if !ok {
		status := f.status
		if status == 0 {
			status = http.StatusNotFound
		}
		http.Error(w, `{"errorMessages": ["secret detail"]}`, status)
		return
	}
	_, _ = fmt.Fprint(w, issue)
}

func issueJSON(summary, description string) string {
	return `{"key": "x", "fields": {"summary": "` + summary + `", "description": ` + description + `}}`
}

// jiraWithStory serves KAN-21 with the house-style description.
func jiraWithStory() *fakeJira {
	return &fakeJira{issues: map[string]string{"KAN-21": issueJSON("Review against the story", storyDescription)}}
}

func jiraEnv(serverURL string) func(string) string {
	env := map[string]string{"JIRA_BASE_URL": serverURL + "/", "JIRA_EMAIL": "dev@example.com", "JIRA_API_TOKEN": "jira-token"}
	return func(name string) string { return env[name] }
}

func readStoryFrom(t *testing.T, jira *fakeJira, key string) (story, error) {
	t.Helper()
	server := httptest.NewServer(jira)
	t.Cleanup(server.Close)
	return newJiraClient(jiraEnv(server.URL), server.Client()).story(t.Context(), key)
}

func TestJiraClientReadsTheStoryAndItsCriteria(t *testing.T) {
	got, err := readStoryFrom(t, jiraWithStory(), "KAN-21")
	if err != nil {
		t.Fatalf("story: %v", err)
	}

	if got.Key != "KAN-21" || got.Summary != "Review against the story" || !got.available() {
		t.Errorf("story = %+v", got)
	}
	if ids := strings.Join(got.criterionIDs(), ","); ids != "AC1,AC2" {
		t.Errorf("criteria = %s, want AC1,AC2", ids)
	}
	if !strings.Contains(got.Description, "## Scope") || !strings.HasPrefix(got.Criteria[0].Text, "**Given** a story key") {
		t.Errorf("story = %+v", got)
	}
}

func TestJiraClientReportsAStoryWithNoCriteria(t *testing.T) {
	jira := &fakeJira{issues: map[string]string{
		"KAN-5":  issueJSON("No criteria", `{"type": "doc", "content": [{"type": "paragraph", "content": [{"type": "text", "text": "Just do it."}]}]}`),
		"KAN-6":  issueJSON("No description", "null"),
		"KAN-21": issueJSON("x", storyDescription),
	}}
	for _, key := range []string{"KAN-5", "KAN-6"} {
		got, err := readStoryFrom(t, jira, key)
		if err != nil {
			t.Fatalf("story %s: %v", key, err)
		}
		if got.available() || got.Unavailable != "story "+key+" has no Acceptance criteria section" {
			t.Errorf("story %s = %+v, want it unavailable for want of criteria", key, got)
		}
	}
}

func TestJiraClientFailures(t *testing.T) {
	for name, tc := range map[string]struct {
		jira   *fakeJira
		key    string
		check  func(error) bool
		reason string
	}{
		"no such story": {&fakeJira{}, "KAN-99", func(err error) bool { return errors.Is(err, errStoryNotFound) },
			"KAN-99: jira has no such story, or the token cannot see it"},
		"token refused": {&fakeJira{status: http.StatusForbidden}, "KAN-99", nil, "jira refused the token (status 403)"},
		"server error":  {&fakeJira{status: http.StatusBadGateway}, "KAN-99", nil, "jira answered status 502"},
		"not JSON":      {&fakeJira{issues: map[string]string{"KAN-1": "<html>"}}, "KAN-1", nil, "decode story KAN-1"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := readStoryFrom(t, tc.jira, tc.key)
			if err == nil || !strings.Contains(err.Error(), tc.reason) || (tc.check != nil && !tc.check(err)) {
				t.Errorf("err = %v, want %q", err, tc.reason)
			}
			if err != nil && strings.Contains(err.Error(), "secret detail") {
				t.Errorf("err = %v repeats Jira's response body", err)
			}
		})
	}
}

func TestJiraClientReportsAnUnreachableJira(t *testing.T) {
	server := httptest.NewServer(jiraWithStory())
	server.Close()

	_, err := newJiraClient(jiraEnv(server.URL), server.Client()).story(t.Context(), "KAN-21")
	if err == nil || !strings.Contains(err.Error(), "jira could not be reached") {
		t.Errorf("err = %v", err)
	}
}

func TestNewJiraClientNeedsEverySetting(t *testing.T) {
	for _, missing := range []string{"JIRA_BASE_URL", "JIRA_EMAIL", "JIRA_API_TOKEN"} {
		env := jiraEnv("https://example.atlassian.net")
		getenv := func(name string) string {
			if name == missing {
				return ""
			}
			return env(name)
		}
		if client := newJiraClient(getenv, http.DefaultClient); client != nil {
			t.Errorf("without %s, newJiraClient = %+v, want nil", missing, client)
		}
	}
}
