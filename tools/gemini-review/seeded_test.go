package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const seededDir = "seeded"

// The stories ask for a seeded case of at least these rules, and a clean
// one: KAN-19 for the first rules, KAN-20 for the Go and Oura rules.
var requiredSeededRules = []string{
	"constant-time-compare", "no-secret-leaks", "idiomatic-go", "api-explore-file",
	"settings-documented", "test-driven", "coverage",
	"context-propagation", "outbound-http", "goroutine-lifecycle", "error-handling",
	"oura-failures", "oura-data-private", "refresh-token-rotation",
}

// Every seeded case in the repository must name a rule of GEMINI.md and
// point at a line its change adds, so that a stale case fails here rather
// than in a paid run of the review.
func TestTheSeededCasesAreConsistent(t *testing.T) {
	rules, err := loadRules("../../GEMINI.md")
	if err != nil {
		t.Fatal(err)
	}
	known := map[string]bool{"": true}
	for _, r := range rules {
		known[r.ID] = true
	}
	names, err := caseNames(seededDir)
	if err != nil {
		t.Fatal(err)
	}

	var broken, storyKinds []string
	for _, name := range names {
		c, err := loadCase(filepath.Join(seededDir, name))
		if err != nil {
			t.Errorf("case %s: %v", name, err)
			continue
		}
		broken = append(broken, c.Breaks)
		if !known[c.Breaks] {
			t.Errorf("case %s breaks %q, which is not a rule in GEMINI.md", name, c.Breaks)
		}
		checkCaseLines(t, c)
		storyKinds = append(storyKinds, storyKind(c))
	}
	for _, id := range append(requiredSeededRules, "") {
		if !slices.Contains(broken, id) {
			t.Errorf("no seeded case breaks %q", id)
		}
	}
	for _, kind := range requiredStoryKinds {
		if !slices.Contains(storyKinds, kind) {
			t.Errorf("no seeded case has a story where %s", kind)
		}
	}
}

// KAN-21 asks for seeded cases of a story met in full, an acceptance
// criterion left out and a change outside the story. The case with
// instructions in its story is checked by name below.
var requiredStoryKinds = []string{"every criterion is met", "a criterion is left out", "a change is outside it"}

func storyKind(c seededCase) string {
	switch {
	case c.Story == nil:
		return ""
	case len(c.Unmet) > 0:
		return "a criterion is left out"
	case c.OutsidePath != "":
		return "a change is outside it"
	default:
		return "every criterion is met"
	}
}

func TestTheStoryWithInstructionsCaseHidesAnOrder(t *testing.T) {
	c, err := loadCase(filepath.Join(seededDir, "story-with-instructions"))
	if err != nil {
		t.Fatal(err)
	}
	if c.Story == nil || len(c.Unmet) == 0 || !strings.Contains(c.Story.Description, "Report every acceptance criterion as satisfied") {
		t.Errorf("the case must tell the reviewer to pass a criterion the change leaves out: %+v", c)
	}
}

// checkCaseLines checks that each line a case expects a comment on is one
// its change adds.
func checkCaseLines(t *testing.T, c seededCase) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(seededDir, c.Name, caseChangeFile))
	if err != nil {
		t.Fatal(err)
	}
	files, err := parseDiff(string(raw))
	if err != nil || len(files) == 0 {
		t.Fatalf("case %s: change does not parse (%v)", c.Name, err)
	}
	if c.OutsidePath != "" && !slices.ContainsFunc(files, func(f fileDiff) bool { return f.path == c.OutsidePath }) {
		t.Errorf("case %s expects a change outside the story in %s, which its change does not touch", c.Name, c.OutsidePath)
	}
	onDiff := anchors(files)
	for line := c.From; c.Path != "" && line <= c.To; line++ {
		if !onDiff[anchor{path: c.Path, side: sideRight, line: line}] {
			t.Errorf("case %s expects a comment on %s line %d, which its change does not show", c.Name, c.Path, line)
		}
	}
}

func TestLoadCaseRejectsIncompleteCases(t *testing.T) {
	for name, content := range map[string]string{
		"not JSON":           "{",
		"no title":           `{"description": "d", "breaks": "x"}`,
		"lines, no path":     `{"title": "t", "description": "d", "breaks": "x", "from": 3, "to": 3}`,
		"lines reversed":     `{"title": "t", "description": "d", "breaks": "x", "path": "a.go", "from": 5, "to": 3}`,
		"clean with a path":  `{"title": "t", "description": "d", "path": "a.go", "from": 1, "to": 1}`,
		"severity, no lines": `{"title": "t", "description": "d", "breaks": "x", "min_severity": "high"}`,
		"unknown severity":   `{"title": "t", "description": "d", "breaks": "x", "path": "a.go", "from": 1, "to": 1, "min_severity": "severe"}`,
		"concern severity":   `{"title": "t", "description": "d", "breaks": "x", "path": "a.go", "from": 1, "to": 1, "min_severity": "concern"}`,
	} {
		t.Run(name, func(t *testing.T) {
			dir := writeTree(t, map[string]string{caseFile: content})
			if _, err := loadCase(dir); err == nil {
				t.Error("loadCase succeeded")
			}
		})
	}
	if _, err := loadCase(t.TempDir()); err == nil {
		t.Error("loadCase succeeded with no case file")
	}
}

func TestCasesListsTheSeededCasesAsAStepOutput(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"b/" + caseFile: "{}", "a/" + caseFile: "{}", "notes/readme.md": "x", "loose.json": "{}",
	})
	outputs := filepath.Join(t.TempDir(), "outputs")
	var out strings.Builder

	if err := casesCommand(dir, outputs, &out); err != nil {
		t.Fatalf("cases: %v", err)
	}

	if got, _ := os.ReadFile(outputs); string(got) != `cases=["a","b"]`+"\n" {
		t.Errorf("outputs = %q", got)
	}
	for _, bad := range []string{"", t.TempDir(), filepath.Join(t.TempDir(), "missing")} {
		if err := casesCommand(bad, "", &out); err == nil {
			t.Errorf("cases succeeded with %q", bad)
		}
	}
}

// seededCaseAt is a seeded case and the review directory seed and the
// facts step leave for it.
type seededCaseAt struct{ caseDir, reviewDir string }

// seededReviewDir seeds a case that breaks the given rule on new.go lines 2
// to 3, or a clean case when breaks is empty, and leaves the facts and
// standards result the facts step would.
func seededReviewDir(t *testing.T, breaks string, findings []finding) seededCaseAt {
	t.Helper()
	c := map[string]any{"title": "Add a helper", "description": "Adds new.go.", "breaks": breaks}
	if breaks != "" {
		c["path"], c["from"], c["to"] = "new.go", 2, 3
	}
	raw, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	caseDir := writeTree(t, map[string]string{
		caseFile:       string(raw),
		caseChangeFile: addedFile("new.go", "package x", "func f() error {", "\treturn err", "}"),
	})
	reviewDir := filepath.Join(t.TempDir(), "review")
	var out strings.Builder
	if err := seedCommand(caseDir, reviewDir, "", &out); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := writeRouting(reviewDir, routing{Tier: tierStandard, Model: testModel}); err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(filepath.Join(reviewDir, factsDataFile), facts{InternalCoverage: "63.4%"}); err != nil {
		t.Fatal(err)
	}
	writeStandardsResult(t, reviewDir, findings)
	return seededCaseAt{caseDir, reviewDir}
}

func TestSeedWritesTheCaseAsAPullRequestWithoutGivingItAway(t *testing.T) {
	dir := seededReviewDir(t, "gemini-rule", nil).reviewDir

	document, err := os.ReadFile(filepath.Join(dir, pullRequestFile))
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{"## Title\n\nAdd a helper", "Adds new.go.", "R3     |+\treturn err"} {
		if !strings.Contains(string(document), part) {
			t.Errorf("%s lacks %q:\n%s", pullRequestFile, part, document)
		}
	}
	if strings.Contains(string(document), "gemini-rule") || strings.Contains(string(document), "seeded") {
		t.Errorf("%s gives the expected finding away:\n%s", pullRequestFile, document)
	}
	if head, _ := os.ReadFile(filepath.Join(dir, headFile)); string(head) != "seeded" {
		t.Errorf("head = %q", head)
	}
}

func TestSeedFailsOnABadCase(t *testing.T) {
	var out strings.Builder
	good := `{"title": "t", "description": "d", "breaks": ""}`
	for name, tc := range map[string]struct {
		files map[string]string
		dir   string
	}{
		"no case named": {nil, ""},
		"no change":     {map[string]string{caseFile: good}, "x"},
		"empty change":  {map[string]string{caseFile: good, caseChangeFile: ""}, "x"},
		"bad change":    {map[string]string{caseFile: good, caseChangeFile: "diff --git a/a b/a\n@@ nonsense\n"}, "x"},
	} {
		t.Run(name, func(t *testing.T) {
			caseDir := tc.dir
			if caseDir != "" {
				caseDir = writeTree(t, tc.files)
			}
			if err := seedCommand(caseDir, t.TempDir(), "o/r", &out); err == nil {
				t.Error("seed succeeded")
			}
		})
	}
}

func evaluate(t *testing.T, dir seededCaseAt, reply string) (string, error) {
	t.Helper()
	var out strings.Builder
	err := evaluateCommand(dir.caseDir, dir.reviewDir, evaluateInputs{
		reply:         reply,
		cliOutput:     cliOutputFile(t, map[string][2]int{testModel: {2, 0}}),
		standardsFile: testStandardsFile(t),
		summary:       filepath.Join(t.TempDir(), "summary.md"),
	}, &out)
	return out.String(), err
}

func replyWith(comments, verdict, reason string) string {
	return `{"summary": "s", "comments": [` + comments + `], "checklist": [{"rule": "gemini-rule", "verdict": "` +
		verdict + `", "reason": "` + reason + `"}]}`
}

func TestEvaluatePassesAReviewThatCaughtTheDefect(t *testing.T) {
	dir := seededReviewDir(t, "gemini-rule", nil)
	reply := replyWith(`{"path": "new.go", "line": 3, "body": "Wrap the error."}`, "fail", "Unwrapped.")

	out, err := evaluate(t, dir, reply)

	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if !strings.Contains(out, "✅ caught") || !strings.Contains(out, "`new.go` line 3: **F1** Wrap the error.") {
		t.Errorf("report = %q", out)
	}
}

func TestEvaluateFailsAReviewThatMissedTheDefect(t *testing.T) {
	for name, tc := range map[string]struct {
		breaks, reply, want string
	}{
		"rule passed": {"gemini-rule", replyWith(`{"path": "new.go", "line": 3, "body": "x"}`, "pass", "Fine."),
			`did not fail gemini-rule (verdict "pass")`},
		"comment on the wrong line": {"gemini-rule", replyWith(`{"path": "new.go", "line": 4, "body": "x"}`, "fail", "x"),
			"no confirmed comment on new.go lines 2 to 3"},
		"only a concern on the rule": {"gemini-rule", replyWith(`{"path": "new.go", "line": 3, "body": "x"}`, "concern", "Maybe."),
			`did not fail gemini-rule (verdict "concern")`},
		"only a concern on the line": {"gemini-rule", replyWith(`{"path": "new.go", "line": 3, "severity": "concern", "body": "x"}`, "fail", "x"),
			"no confirmed comment on new.go lines 2 to 3"},
		"clean change with a concern comment": {"", replyWith(`{"path": "new.go", "line": 1, "severity": "concern", "body": "Maybe."}`, "pass", "Fine."),
			"the review made 1 comments"},
		"clean change with a concern verdict": {"", replyWith("", "concern", "Maybe."), "raised a concern on gemini-rule"},
		"clean change commented on": {"", replyWith(`{"path": "new.go", "line": 1, "body": "Nit."}`, "pass", "Fine."),
			"the review made 1 comments"},
		"clean change failed": {"", replyWith("", "fail", "Why not."), "the review failed gemini-rule"},
	} {
		t.Run(name, func(t *testing.T) {
			dir := seededReviewDir(t, tc.breaks, nil)
			out, err := evaluate(t, dir, tc.reply)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want it to contain %q", err, tc.want)
			}
			if !strings.Contains(out, "❌ missed") {
				t.Errorf("report = %q, want it marked missed", out)
			}
		})
	}
}

func TestEvaluateChecksTheCommentIsAsSevereAsTheCaseAsks(t *testing.T) {
	dir := seededReviewDir(t, "gemini-rule", nil)
	rewriteCase(t, dir, func(c *seededCase) { c.MinSeverity = severityHigh })
	comment := func(severity string) string {
		return `{"path": "new.go", "line": 3, "severity": "` + severity + `", "body": "Leaks the token."}`
	}

	for _, enough := range []string{"high", "critical"} {
		if _, err := evaluate(t, dir, replyWith(comment(enough), "fail", "Logged.")); err != nil {
			t.Errorf("evaluate with a %s comment: %v", enough, err)
		}
	}
	for _, tooLow := range []string{"medium", "low", "concern", ""} {
		_, err := evaluate(t, dir, replyWith(comment(tooLow), "fail", "Logged."))
		if err == nil || !strings.Contains(err.Error(), "no confirmed comment of high or higher severity on new.go lines 2 to 3") {
			t.Errorf("evaluate with a %q comment: err = %v", tooLow, err)
		}
	}
}

func TestEvaluateCountsAFindingOfTheStandardsCheck(t *testing.T) {
	dir := seededReviewDir(t, "script-rule", []finding{{Rule: "script-rule", Path: "new.go", Line: 2, Side: sideRight, Message: "m"}})

	if _, err := evaluate(t, dir, replyWith("", "pass", "Fine.")); err != nil {
		t.Errorf("evaluate: %v", err)
	}
}

func TestEvaluateChecksTheCoverageIsCited(t *testing.T) {
	dir := seededReviewDir(t, "gemini-rule", nil)
	rewriteCase(t, dir, func(c *seededCase) { c.CitesCoverage = true })

	comment := `{"path": "new.go", "line": 2, "body": "Coverage falls."}`
	if _, err := evaluate(t, dir, replyWith(comment, "fail", "Coverage of internal/ is 63.4%, below 85%.")); err != nil {
		t.Errorf("evaluate with the coverage cited: %v", err)
	}
	if _, err := evaluate(t, dir, replyWith(comment, "fail", "Coverage dropped.")); err == nil || !strings.Contains(err.Error(), "cite the computed coverage, 63.4%") {
		t.Errorf("evaluate without the coverage cited: err = %v", err)
	}
}

func TestEvaluateFailsWithoutAUsableReviewOrItsInputs(t *testing.T) {
	good := replyWith("", "pass", "Fine.")
	for name, tc := range map[string]struct {
		remove, reply string
	}{
		"unusable reply":     {"", "Sorry."},
		"checklist omits":    {"", `{"summary": "s", "checklist": []}`},
		"no seeded case":     {caseFile, good},
		"no facts":           {factsDataFile, good},
		"no routing":         {routingFile, good},
		"no standards check": {standardsResultFile, good},
	} {
		t.Run(name, func(t *testing.T) {
			dir := seededReviewDir(t, "", nil)
			switch tc.remove {
			case "":
			case caseFile:
				dir.caseDir = t.TempDir()
			default:
				if err := os.Remove(filepath.Join(dir.reviewDir, tc.remove)); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := evaluate(t, dir, tc.reply); err == nil {
				t.Error("evaluate succeeded")
			}
		})
	}
}

func TestEvaluateFailsWhenTheRoutedModelDidNotAnswer(t *testing.T) {
	dir := seededReviewDir(t, "", nil)
	var out strings.Builder
	err := evaluateCommand(dir.caseDir, dir.reviewDir, evaluateInputs{
		reply:         replyWith("", "pass", "Fine."),
		cliOutput:     cliOutputFile(t, map[string][2]int{"another-model": {1, 0}}),
		standardsFile: testStandardsFile(t),
	}, &out)
	if err == nil || !strings.Contains(err.Error(), "did not answer") {
		t.Errorf("err = %v", err)
	}
}

func rewriteCase(t *testing.T, dir seededCaseAt, change func(*seededCase)) {
	t.Helper()
	c, err := loadCase(dir.caseDir)
	if err != nil {
		t.Fatal(err)
	}
	change(&c)
	if err := writeJSON(filepath.Join(dir.caseDir, caseFile), c); err != nil {
		t.Fatal(err)
	}
}

// storyCase gives a seeded case a story with criteria AC1 and AC2, and the
// expectations given.
func storyCase(unmet []string, outsidePath string) func(*seededCase) {
	return func(c *seededCase) {
		c.Story = &seededStory{Key: "KAN-90", Summary: "Add a helper", Description: "Adds new.go.",
			Criteria: []string{"Given x When f runs Then it returns err", "Given y When f runs Then it logs nothing"}}
		c.Unmet = unmet
		c.OutsidePath = outsidePath
	}
}

// reseed seeds the case again after a change, as the workflow would.
func reseed(t *testing.T, dir seededCaseAt) {
	t.Helper()
	if err := seedCommand(dir.caseDir, dir.reviewDir, "", &strings.Builder{}); err != nil {
		t.Fatalf("seed: %v", err)
	}
}

func storyReplyWith(ac1, ac2, outside string) string {
	return `{"summary": "s", "checklist": [{"rule": "gemini-rule", "verdict": "pass", "reason": "Fine."}], "story": {"criteria": [` +
		`{"id": "AC1", "quote": "it returns err", "verdict": "` + ac1 + `", "reason": "r"}, ` +
		`{"id": "AC2", "quote": "it logs nothing", "verdict": "` + ac2 + `", "reason": "r"}], "outside": [` + outside + `]}}`
}

func TestSeedWritesTheStoryWithoutTheExpectations(t *testing.T) {
	dir := seededReviewDir(t, "", nil)
	rewriteCase(t, dir, storyCase([]string{"AC2"}, "new.go"))
	reseed(t, dir)

	document := readText(t, filepath.Join(dir.reviewDir, storyFile))
	if !strings.Contains(document, "# Story KAN-90") || !strings.Contains(document, "### AC2\n\nGiven y When f runs Then it logs nothing") {
		t.Errorf("%s:\n%s", storyFile, document)
	}
	for _, giveaway := range []string{"unmet", "outside_path", "seeded"} {
		if strings.Contains(document, giveaway) {
			t.Errorf("%s gives the expectation away with %q:\n%s", storyFile, giveaway, document)
		}
	}
}

func TestSeedSaysACaseWithoutAStoryHasNoCriteria(t *testing.T) {
	dir := seededReviewDir(t, "", nil)
	got, err := readStory(dir.reviewDir)
	if err != nil || got.available() || strings.Contains(got.Unavailable, "seeded") {
		t.Errorf("story = %+v, %v", got, err)
	}
}

func TestEvaluatePassesAReviewThatJudgedTheStoryRight(t *testing.T) {
	for name, tc := range map[string]struct {
		unmet   []string
		outside string
		reply   string
	}{
		"every criterion met": {nil, "", storyReplyWith("satisfied", "satisfied", "")},
		"criterion left out":  {[]string{"AC2"}, "", storyReplyWith("satisfied", "not_satisfied", "")},
		"change outside": {nil, "new.go", storyReplyWith("satisfied", "satisfied",
			`{"path": "new.go", "line": 3, "change": "Adds a log line."}`)},
	} {
		t.Run(name, func(t *testing.T) {
			dir := seededReviewDir(t, "", nil)
			rewriteCase(t, dir, storyCase(tc.unmet, tc.outside))
			reseed(t, dir)

			out, err := evaluate(t, dir, tc.reply)
			if err != nil {
				t.Fatalf("evaluate: %v\n%s", err, out)
			}
			if !strings.Contains(out, "### Acceptance criteria of KAN-90") {
				t.Errorf("report lacks the story's verdicts:\n%s", out)
			}
		})
	}
}

func TestEvaluateFailsAReviewThatMisjudgedTheStory(t *testing.T) {
	for name, tc := range map[string]struct {
		unmet   []string
		outside string
		reply   string
		want    string
	}{
		"left-out criterion passed": {[]string{"AC2"}, "", storyReplyWith("satisfied", "satisfied", ""),
			`the review judged AC2 "satisfied", want "not_satisfied"`},
		"left-out criterion only partly": {[]string{"AC2"}, "", storyReplyWith("satisfied", "partly", ""),
			`the review judged AC2 "partly", want "not_satisfied"`},
		"met criterion failed": {nil, "", storyReplyWith("not_satisfied", "satisfied", ""),
			`the review judged AC1 "not_satisfied", want "satisfied"`},
		"change outside missed": {nil, "new.go", storyReplyWith("satisfied", "satisfied", ""),
			"the review lists no change in new.go as outside the story"},
		"change outside invented": {nil, "", storyReplyWith("satisfied", "satisfied", `{"path": "new.go", "line": 1, "change": "x"}`),
			"every change serves the story, but the review lists 1 as outside it"},
	} {
		t.Run(name, func(t *testing.T) {
			dir := seededReviewDir(t, "", nil)
			rewriteCase(t, dir, storyCase(tc.unmet, tc.outside))
			reseed(t, dir)

			out, err := evaluate(t, dir, tc.reply)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want %q", err, tc.want)
			}
			if !strings.Contains(out, "❌ missed") {
				t.Errorf("report = %q", out)
			}
		})
	}
}

func TestEvaluateReportSaysWhatTheStoryCaseExpects(t *testing.T) {
	for name, tc := range map[string]struct {
		unmet   []string
		outside string
		want    string
	}{
		"all met":    {nil, "", "; every acceptance criterion satisfied; nothing outside the story"},
		"left out":   {[]string{"AC2"}, "", "; AC2 not satisfied and every other criterion satisfied; nothing outside the story"},
		"outside it": {nil, "new.go", "; every acceptance criterion satisfied; a change in `new.go` listed as outside the story"},
	} {
		t.Run(name, func(t *testing.T) {
			c := seededCase{}
			storyCase(tc.unmet, tc.outside)(&c)
			if got := c.storyExpected(); got != tc.want {
				t.Errorf("storyExpected = %q, want %q", got, tc.want)
			}
		})
	}
	if got := (seededCase{}).storyExpected(); got != "" {
		t.Errorf("storyExpected without a story = %q", got)
	}
}

func TestLoadCaseRejectsABadStory(t *testing.T) {
	story := `"story": {"key": "KAN-90", "summary": "s", "description": "d", "criteria": ["c"]}`
	for name, content := range map[string]string{
		"unmet, no story":   `{"title": "t", "description": "d", "unmet": ["AC1"]}`,
		"outside, no story": `{"title": "t", "description": "d", "outside_path": "a.go"}`,
		"no criteria":       `{"title": "t", "description": "d", "story": {"key": "KAN-90", "summary": "s", "criteria": []}}`,
		"no key":            `{"title": "t", "description": "d", "story": {"summary": "s", "criteria": ["c"]}}`,
		"unknown unmet":     `{"title": "t", "description": "d", ` + story + `, "unmet": ["AC2"]}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := loadCase(writeTree(t, map[string]string{caseFile: content})); err == nil {
				t.Error("loadCase succeeded")
			}
		})
	}
}
