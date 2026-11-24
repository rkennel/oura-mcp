package main

import (
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

// prepareStory prepares pull request 7 with the Jira settings pointing at
// the fake's own server, and returns the story it wrote and the log.
func prepareStory(t *testing.T, fake *fakeGitHub, jiraSettings bool) (story, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "review")
	extra := map[string]string{}
	if jiraSettings {
		extra = map[string]string{"JIRA_EMAIL": "dev@example.com", "JIRA_API_TOKEN": "jira-token", "JIRA_BASE_URL": "self"}
	}
	out, err := runToolWithJira(t, fake, dir, extra, "prepare")
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	got, err := readStory(dir)
	if err != nil {
		t.Fatal(err)
	}
	return got, out
}

// runToolWithJira is runTool with JIRA_BASE_URL "self" meaning the fake's
// own server, which serves Jira's paths too.
func runToolWithJira(t *testing.T, fake *fakeGitHub, dir string, extra map[string]string, args ...string) (string, error) {
	t.Helper()
	server := newFakeServer(t, fake)
	if extra["JIRA_BASE_URL"] == "self" {
		extra = with(extra, "JIRA_BASE_URL", server.URL)
	}
	var out strings.Builder
	err := run(t.Context(), args, testEnv(server.URL, dir, extra), server.Client(), &out)
	return out.String(), err
}

func TestPrepareReadsTheStoryNamedInTheTitle(t *testing.T) {
	jira := jiraWithStory()
	got, out := prepareStory(t, &fakeGitHub{title: "KAN-21: Review against the story", jira: jira}, true)

	if !got.available() || got.Key != "KAN-21" || len(got.Criteria) != 2 {
		t.Errorf("story = %+v", got)
	}
	if !strings.Contains(out, "Story KAN-21 has 2 acceptance criteria.") {
		t.Errorf("output = %q", out)
	}
}

func TestPrepareFallsBackToTheBranchAndTheFirstCommit(t *testing.T) {
	for name, fake := range map[string]*fakeGitHub{
		"branch":       {branch: "feature/kan-21-review"},
		"first commit": {branch: "claude/project-thread-y29nxf", firstCommit: "KAN-21: Review against the story\n\nBody."},
	} {
		t.Run(name, func(t *testing.T) {
			fake.jira = jiraWithStory()
			if got, _ := prepareStory(t, fake, true); got.Key != "KAN-21" || !got.available() {
				t.Errorf("story = %+v", got)
			}
		})
	}
}

func TestPrepareRunsWithoutTheStoryAndSaysWhy(t *testing.T) {
	for name, tc := range map[string]struct {
		fake         *fakeGitHub
		jiraSettings bool
		why          string
	}{
		"no key": {&fakeGitHub{firstCommit: "Trim PORT", jira: jiraWithStory()}, true,
			"no story key such as KAN-21 starts the pull request's title or first commit message, or a part of its branch name"},
		"commits unreadable": {&fakeGitHub{commitsStatus: http.StatusBadGateway, jira: jiraWithStory()}, true,
			"the pull request's title and branch name name no story, and its first commit could not be read"},
		"no Jira settings": {&fakeGitHub{title: "KAN-21: x", jira: jiraWithStory()}, false,
			"story KAN-21 was not read because the review has no Jira access"},
		"story not found": {&fakeGitHub{title: "KAN-99: x", jira: jiraWithStory()}, true,
			"story KAN-99 could not be read from Jira: KAN-99: jira has no such story"},
		"Jira down": {&fakeGitHub{title: "KAN-99: x", jira: &fakeJira{status: http.StatusServiceUnavailable}}, true,
			"story KAN-99 could not be read from Jira: jira answered status 503"},
	} {
		t.Run(name, func(t *testing.T) {
			got, out := prepareStory(t, tc.fake, tc.jiraSettings)
			if got.available() || !strings.Contains(got.Unavailable, tc.why) {
				t.Errorf("story = %+v, want it unavailable because %q", got, tc.why)
			}
			if !strings.Contains(out, "No acceptance criteria: "+tc.why) {
				t.Errorf("output = %q", out)
			}
		})
	}
}
