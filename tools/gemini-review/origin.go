package main

import (
	"context"
	"fmt"
	"io"
	"strings"
)

// origin is where a pull request's head branch lives.
type origin struct {
	repo     string // the repository running the workflow, owner/name
	headRepo string // the head branch's repository; empty when it was deleted
}

// sameRepository reports whether the head branch is in the repository that
// runs the workflow. GitHub treats owner and repository names
// case-insensitively. A deleted head repository counts as a fork, because
// nobody can say whose code it was.
func (o origin) sameRepository() bool {
	return o.headRepo != "" && strings.EqualFold(o.headRepo, o.repo)
}

func (o origin) headDescription() string {
	if o.headRepo == "" {
		return "a repository that no longer exists"
	}
	return "`" + o.headRepo + "`"
}

// originCommand decides whether the review may run. It runs in a job of its
// own, with no secrets, before the review job checks out the pull request: a
// pull request from a fork must not have its code run with the Gemini key or
// a token that can write, whatever started the workflow. It writes
// same_repo=true|false to the step outputs and, when it skips, says why in
// the job summary.
func originCommand(ctx context.Context, cfg config, outputs, summary string, out io.Writer) error {
	pr, err := cfg.client.pullRequest(ctx, cfg.prNumber)
	if err != nil {
		return err
	}
	o := origin{repo: cfg.client.repo, headRepo: pr.Head.Repo.FullName}
	if o.sameRepository() {
		fmt.Fprintf(out, "Pull request #%d comes from a branch in %s; the review runs.\n", cfg.prNumber, o.repo)
	} else {
		fmt.Fprintf(out, "::notice title=Gemini review skipped::Pull request #%d comes from %s, not %s.\n", cfg.prNumber, o.headDescription(), o.repo)
		if err := appendFile(summary, skippedSummary(cfg.prNumber, o)); err != nil {
			return err
		}
	}
	return appendFile(outputs, fmt.Sprintf("same_repo=%t\n", o.sameRepository()))
}

// skippedSummary is the job summary when the review does not run.
func skippedSummary(number int, o origin) string {
	return fmt.Sprintf("### Gemini review skipped\n\n"+
		"Pull request #%d comes from %s, not from a branch in `%s`. "+
		"The review builds and runs code from the pull request with the Gemini API key "+
		"and a token that can write, so it runs only on pull requests from this repository's own branches. "+
		"To have it reviewed, push the branch to this repository and open the pull request from there.\n", number, o.headDescription(), o.repo)
}
