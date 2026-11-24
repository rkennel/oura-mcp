package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
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

// githubUser is the author of a review or comment.
type githubUser struct {
	Login string `json:"login"`
}

// githubReview is a review on a pull request, as GitHub lists it.
type githubReview struct {
	ID          int64      `json:"id"`
	User        githubUser `json:"user"`
	Body        string     `json:"body"`
	CommitID    string     `json:"commit_id"`
	HTMLURL     string     `json:"html_url"`
	SubmittedAt time.Time  `json:"submitted_at"` // zero for a review not yet submitted
}

// githubComment is a comment on a pull request: a line comment of a review,
// a reply in its thread, or a comment in the conversation.
type githubComment struct {
	ID          int64      `json:"id"`
	InReplyToID int64      `json:"in_reply_to_id"` // the thread's first comment; 0 when it starts one or is in the conversation
	User        githubUser `json:"user"`
	Body        string     `json:"body"`
	HTMLURL     string     `json:"html_url"`
	CreatedAt   time.Time  `json:"created_at"`
}

// commitStatus is the body of GitHub's "create a commit status" call.
type commitStatus struct {
	State       string `json:"state"` // success or failure
	Description string `json:"description"`
	Context     string `json:"context"`
	TargetURL   string `json:"target_url,omitempty"`
}

// reviews lists the pull request's reviews, oldest first.
func (c *githubClient) reviews(ctx context.Context, number int) ([]githubReview, error) {
	return listAll[githubReview](ctx, c, c.pullPath(number, "/reviews"))
}

// reviewComments lists the line comments a review posted.
func (c *githubClient) reviewComments(ctx context.Context, number int, reviewID int64) ([]githubComment, error) {
	return listAll[githubComment](ctx, c, c.pullPath(number, fmt.Sprintf("/reviews/%d/comments", reviewID)))
}

// lineComments lists every line comment on the pull request, replies
// included.
func (c *githubClient) lineComments(ctx context.Context, number int) ([]githubComment, error) {
	return listAll[githubComment](ctx, c, c.pullPath(number, "/comments"))
}

// conversationComments lists the comments in the pull request's
// conversation, which GitHub keeps as the comments of its issue.
func (c *githubClient) conversationComments(ctx context.Context, number int) ([]githubComment, error) {
	return listAll[githubComment](ctx, c, fmt.Sprintf("/repos/%s/issues/%d/comments", c.repo, number))
}

// createStatus sets a commit status on sha.
func (c *githubClient) createStatus(ctx context.Context, sha string, status commitStatus) error {
	payload, err := json.Marshal(status)
	if err != nil {
		return fmt.Errorf("encode commit status: %w", err)
	}
	_, err = c.do(ctx, http.MethodPost, fmt.Sprintf("/repos/%s/statuses/%s", c.repo, sha), "application/vnd.github+json", payload)
	return err
}

// Lists are read a page at a time. maxPages bounds a list that never ends,
// far beyond what a pull request has.
const (
	pageSize = 100
	maxPages = 50
)

// listAll reads every page of a GitHub list at path.
func listAll[T any](ctx context.Context, c *githubClient, path string) ([]T, error) {
	var all []T
	for page := 1; page <= maxPages; page++ {
		body, err := c.do(ctx, http.MethodGet, fmt.Sprintf("%s?per_page=%d&page=%d", path, pageSize, page), "application/vnd.github+json", nil)
		if err != nil {
			return nil, err
		}
		var items []T
		if err := json.Unmarshal(body, &items); err != nil {
			return nil, fmt.Errorf("decode %s page %d: %w", path, page, err)
		}
		all = append(all, items...)
		if len(items) < pageSize {
			return all, nil
		}
	}
	return nil, fmt.Errorf("%s has more than %d pages", path, maxPages)
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
