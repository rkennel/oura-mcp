package main

import (
	"context"
	"fmt"
	"io"
	"strings"
)

// gateContext names the commit status the gate sets. Branch protection
// requires it by this name, so keep the README in step.
const gateContext = "Gemini findings"

// reviewerLogin is the account the workflow posts the review as. Only its
// reviews count, so nobody else can post a review that passes the gate.
const reviewerLogin = "github-actions[bot]"

// gateSettings are what the gate needs besides the GitHub client.
type gateSettings struct {
	routingConfig string // the file with blocking_severity
	summary       string // the job summary file; empty outside Actions
	runURL        string // the run's page, which the status links to; may be empty
}

// gateCommand sets the Gemini findings status on the pull request's head
// commit: failure while a finding at the configured severity is open, or
// when there is no review to judge, and success otherwise. The status, not
// the job, carries the verdict. A job started by a comment is attached to
// the default branch, not to the pull request, so only a status on the head
// commit can turn green when a finding is dismissed. The job fails only
// when the gate cannot reach a verdict.
func gateCommand(ctx context.Context, cfg config, settings gateSettings, out io.Writer) error {
	routing, err := loadRoutingConfig(settings.routingConfig)
	if err != nil {
		return err
	}
	pr, err := cfg.client.pullRequest(ctx, cfg.prNumber)
	if err != nil {
		return err
	}
	head := pr.Head.SHA
	review, found, err := readPostedReview(ctx, cfg, head)
	if err != nil {
		return err
	}
	var posted *postedReview
	var comments []githubComment
	if found {
		posted = &review
		if comments, err = dismissalCandidates(ctx, cfg); err != nil {
			return err
		}
	}
	result := evaluateGate(head, routing.BlockingSeverity, repositoryOwner(cfg.client.repo), posted, comments)
	status := commitStatus{State: result.state(), Description: result.description(), Context: gateContext, TargetURL: settings.runURL}
	if err := cfg.client.createStatus(ctx, head, status); err != nil {
		return fmt.Errorf("set the %s status: %w", gateContext, err)
	}
	fmt.Fprintf(out, "Set %s on %s to %s: %s\n", gateContext, shortSHA(head), status.State, status.Description)
	return appendFile(settings.summary, result.summary())
}

// readPostedReview reads the latest Gemini review of head and which of its
// line comments starts each finding's thread. found is false when there is
// no such review.
func readPostedReview(ctx context.Context, cfg config, head string) (review postedReview, found bool, err error) {
	reviews, err := cfg.client.reviews(ctx, cfg.prNumber)
	if err != nil {
		return postedReview{}, false, err
	}
	latest, findings, found, err := latestReview(reviews, head)
	if err != nil || !found {
		return postedReview{}, false, err
	}
	comments, err := cfg.client.reviewComments(ctx, cfg.prNumber, latest.ID)
	if err != nil {
		return postedReview{}, false, err
	}
	review = postedReview{review: latest, findings: findings, threads: map[int64]string{}, links: map[string]string{}}
	for _, c := range comments {
		if match := labelledIDPattern.FindStringSubmatch(c.Body); match != nil {
			review.threads[c.ID] = match[1]
			review.links[match[1]] = c.HTMLURL
		}
	}
	return review, true, nil
}

// dismissalCandidates lists every comment that could dismiss a finding:
// the line comments with their replies, and the conversation.
func dismissalCandidates(ctx context.Context, cfg config) ([]githubComment, error) {
	lines, err := cfg.client.lineComments(ctx, cfg.prNumber)
	if err != nil {
		return nil, err
	}
	conversation, err := cfg.client.conversationComments(ctx, cfg.prNumber)
	if err != nil {
		return nil, err
	}
	return append(lines, conversation...), nil
}

// repositoryOwner is the owner part of owner/name.
func repositoryOwner(repo string) string {
	owner, _, _ := strings.Cut(repo, "/")
	return owner
}

// runURL is the address of the workflow run, built from what Actions sets,
// or "" outside Actions.
func runURL(getenv func(string) string) string {
	server, repo, id := getenv("GITHUB_SERVER_URL"), getenv("GITHUB_REPOSITORY"), getenv("GITHUB_RUN_ID")
	if server == "" || repo == "" || id == "" {
		return ""
	}
	return fmt.Sprintf("%s/%s/actions/runs/%s", strings.TrimSuffix(server, "/"), repo, id)
}
