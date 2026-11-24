package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// githubClient makes the few GitHub REST calls the review needs.
type githubClient struct {
	baseURL string // e.g. https://api.github.com
	repo    string // owner/name
	token   string
	http    *http.Client
}

// pullRequest is the part of a pull request the review uses.
type pullRequest struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	Body   string `json:"body"`
	Head   struct {
		SHA  string `json:"sha"`
		Ref  string `json:"ref"` // the branch name
		Repo struct {
			FullName string `json:"full_name"` // owner/name; empty when the repository was deleted
		} `json:"repo"`
	} `json:"head"`
}

// apiError is a GitHub response with a non-2xx status.
type apiError struct {
	method, path string
	status       int
	body         string
}

func (e *apiError) Error() string {
	return fmt.Sprintf("%s %s: status %d: %s", e.method, e.path, e.status, e.body)
}

// isUnprocessable reports whether GitHub rejected a request's content, as it
// does for a review comment on a line it cannot place.
func isUnprocessable(err error) bool {
	var apiErr *apiError
	return errors.As(err, &apiErr) && apiErr.status == http.StatusUnprocessableEntity
}

func (c *githubClient) pullRequest(ctx context.Context, number int) (pullRequest, error) {
	var pr pullRequest
	body, err := c.do(ctx, http.MethodGet, c.pullPath(number, ""), "application/vnd.github+json", nil)
	if err != nil {
		return pr, err
	}
	if err := json.Unmarshal(body, &pr); err != nil {
		return pr, fmt.Errorf("decode pull request %d: %w", number, err)
	}
	return pr, nil
}

func (c *githubClient) diff(ctx context.Context, number int) (string, error) {
	body, err := c.do(ctx, http.MethodGet, c.pullPath(number, ""), "application/vnd.github.diff", nil)
	return string(body), err
}

// firstCommitMessage returns the message of the pull request's first
// commit, or "" when it has none.
func (c *githubClient) firstCommitMessage(ctx context.Context, number int) (string, error) {
	body, err := c.do(ctx, http.MethodGet, c.pullPath(number, "/commits?per_page=1"), "application/vnd.github+json", nil)
	if err != nil {
		return "", err
	}
	var commits []struct {
		Commit struct {
			Message string `json:"message"`
		} `json:"commit"`
	}
	if err := json.Unmarshal(body, &commits); err != nil {
		return "", fmt.Errorf("decode the commits of pull request %d: %w", number, err)
	}
	if len(commits) == 0 {
		return "", nil
	}
	return commits[0].Commit.Message, nil
}

// createReview posts a submitted review and returns its web address.
func (c *githubClient) createReview(ctx context.Context, number int, review reviewRequest) (string, error) {
	payload, err := json.Marshal(review)
	if err != nil {
		return "", fmt.Errorf("encode review: %w", err)
	}
	body, err := c.do(ctx, http.MethodPost, c.pullPath(number, "/reviews"), "application/vnd.github+json", payload)
	if err != nil {
		return "", err
	}
	var created struct {
		HTMLURL string `json:"html_url"`
	}
	if err := json.Unmarshal(body, &created); err != nil {
		return "", fmt.Errorf("decode created review: %w", err)
	}
	return created.HTMLURL, nil
}

func (c *githubClient) pullPath(number int, suffix string) string {
	return fmt.Sprintf("/repos/%s/pulls/%d%s", c.repo, number, suffix)
}

func (c *githubClient) do(ctx context.Context, method, path, accept string, payload []byte) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("build %s %s: %w", method, path, err)
	}
	request.Header.Set("Accept", accept)
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}

	response, err := c.http.Do(request)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("read %s %s: %w", method, path, err)
	}
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return nil, &apiError{method: method, path: path, status: response.StatusCode, body: truncate(string(body), 500)}
	}
	return body, nil
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
