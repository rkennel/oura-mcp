package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// checkOrigin runs origin against a pull request whose head branch is in
// headRepo, and returns the step outputs, the job summary and the log. The
// workflow runs in o/r.
func checkOrigin(t *testing.T, headRepo string) (outputs, summary, log string) {
	t.Helper()
	env := map[string]string{
		"REVIEW_DIR":          "", // origin runs before anything is prepared
		"GITHUB_OUTPUT":       filepath.Join(t.TempDir(), "output"),
		"GITHUB_STEP_SUMMARY": filepath.Join(t.TempDir(), "summary.md"),
	}
	log, err := runTool(t, &fakeGitHub{headRepo: headRepo}, "", env, "origin")
	if err != nil {
		t.Fatalf("origin: %v", err)
	}
	return readText(t, env["GITHUB_OUTPUT"]), readOptional(t, env["GITHUB_STEP_SUMMARY"]), log
}

func readOptional(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(name)
	if os.IsNotExist(err) {
		return ""
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestOriginLetsThisRepositorysBranchesThrough(t *testing.T) {
	for name, headRepo := range map[string]string{
		"same name":      "o/r",
		"different case": "O/R",
	} {
		t.Run(name, func(t *testing.T) {
			outputs, summary, log := checkOrigin(t, headRepo)

			if outputs != "same_repo=true\n" {
				t.Errorf("outputs = %q", outputs)
			}
			if summary != "" {
				t.Errorf("summary = %q, want nothing when the review runs", summary)
			}
			if !strings.Contains(log, "the review runs") {
				t.Errorf("log = %q", log)
			}
		})
	}
}

func TestOriginSkipsPullRequestsFromElsewhere(t *testing.T) {
	for name, tc := range map[string]struct {
		headRepo, describedAs string
	}{
		"fork":                  {"stranger/r", "`stranger/r`"},
		"fork of the same name": {"o/r-fork", "`o/r-fork`"},
		"deleted fork":          {"", "a repository that no longer exists"},
	} {
		t.Run(name, func(t *testing.T) {
			outputs, summary, log := checkOrigin(t, tc.headRepo)

			if outputs != "same_repo=false\n" {
				t.Errorf("outputs = %q", outputs)
			}
			for _, part := range []string{"### Gemini review skipped", "Pull request #7 comes from " + tc.describedAs + ", not from a branch in `o/r`", "Gemini API key"} {
				if !strings.Contains(summary, part) {
					t.Errorf("summary lacks %q:\n%s", part, summary)
				}
			}
			if !strings.HasPrefix(log, "::notice title=Gemini review skipped::") {
				t.Errorf("log = %q, want a notice", log)
			}
		})
	}
}

func TestOriginFailsWhenGitHubRefuses(t *testing.T) {
	_, err := runTool(t, &fakeGitHub{headRepo: "o/r"}, "", map[string]string{"GITHUB_TOKEN": "wrong"}, "origin")
	if err == nil || !strings.Contains(err.Error(), "status 401") {
		t.Errorf("err = %v, want the 401 from GitHub", err)
	}
}
