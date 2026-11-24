package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// jiraTimeout bounds the story lookup, so that a slow Jira leaves the rest
// of prepare's time for GitHub.
const jiraTimeout = 30 * time.Second

// errStoryNotFound means Jira has no story with the key, or the token
// cannot see it: Jira answers 404 to both.
var errStoryNotFound = errors.New("jira has no such story, or the token cannot see it")

// jiraClient reads one story with a read-only API token. It never writes to
// Jira.
type jiraClient struct {
	baseURL string // https://<site>.atlassian.net, or https://api.atlassian.com/ex/jira/<cloud id> for a scoped token
	email   string
	token   string
	http    *http.Client
}

// jiraIssue is the part of a Jira issue the review uses. The description is
// in the Atlassian Document Format, as version 3 of the API returns it.
type jiraIssue struct {
	Fields struct {
		Summary     string   `json:"summary"`
		Description *adfNode `json:"description"`
	} `json:"fields"`
}

// newJiraClient returns a client for the settings, or nil when they are not
// all set, as on a repository that has not set up the Jira secrets.
func newJiraClient(getenv func(string) string, httpClient *http.Client) *jiraClient {
	client := &jiraClient{
		baseURL: strings.TrimSuffix(getenv("JIRA_BASE_URL"), "/"),
		email:   getenv("JIRA_EMAIL"),
		token:   getenv("JIRA_API_TOKEN"),
		http:    httpClient,
	}
	if client.baseURL == "" || client.email == "" || client.token == "" {
		return nil
	}
	return client
}

// story fetches the story's summary and description and reads its
// acceptance criteria out of the description.
func (c *jiraClient) story(ctx context.Context, key string) (story, error) {
	ctx, cancel := context.WithTimeout(ctx, jiraTimeout)
	defer cancel()
	issue, err := c.issue(ctx, key)
	if err != nil {
		return story{}, err
	}
	s := story{
		Key:         key,
		Summary:     strings.TrimSpace(issue.Fields.Summary),
		Description: renderADF(issue.Fields.Description),
		Criteria:    numberCriteria(acceptanceCriteria(issue.Fields.Description)),
	}
	if len(s.Criteria) == 0 {
		s.Unavailable = fmt.Sprintf("story %s has no Acceptance criteria section", key)
	}
	return s, nil
}

func (c *jiraClient) issue(ctx context.Context, key string) (jiraIssue, error) {
	var issue jiraIssue
	address := c.baseURL + "/rest/api/3/issue/" + url.PathEscape(key) + "?fields=summary,description"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return issue, fmt.Errorf("build the Jira request: %w", err)
	}
	request.SetBasicAuth(c.email, c.token)
	request.Header.Set("Accept", "application/json")

	response, err := c.http.Do(request)
	if err != nil {
		return issue, fmt.Errorf("jira could not be reached: %w", err)
	}
	defer response.Body.Close()

	// Jira's error bodies are not read: they say nothing the status does
	// not, and the review should not repeat what an outside service wrote.
	switch {
	case response.StatusCode == http.StatusNotFound:
		return issue, fmt.Errorf("%s: %w", key, errStoryNotFound)
	case response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden:
		return issue, fmt.Errorf("jira refused the token (status %d)", response.StatusCode)
	case response.StatusCode < 200 || response.StatusCode > 299:
		return issue, fmt.Errorf("jira answered status %d", response.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&issue); err != nil {
		return issue, fmt.Errorf("decode story %s: %w", key, err)
	}
	return issue, nil
}
