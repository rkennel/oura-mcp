package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

// Files of a seeded case: a known change, and what the review must say
// about it.
const (
	caseFile       = "case.json"
	caseChangeFile = "change.diff"
)

// seededCase is a known change and what the review must say about it. A
// case that breaks a rule names it, and the review must fail that rule and,
// when Path is set, make a confirmed comment on Path between lines From and
// To. A concern does not count: the seeded defects are provable. A case that
// breaks nothing must get no comment, not even a concern, and no failed rule
// or concern.
type seededCase struct {
	Name        string `json:"name"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Breaks      string `json:"breaks"`
	Path        string `json:"path,omitempty"`
	From        int    `json:"from,omitempty"`
	To          int    `json:"to,omitempty"`
	// MinSeverity asks that the comment on the case's lines be at least this
	// severe, such as high for a security problem.
	MinSeverity string `json:"min_severity,omitempty"`
	// CitesCoverage asks that the verdict's reason give the computed
	// coverage of internal/ from the facts.
	CitesCoverage bool `json:"cites_coverage,omitempty"`
	// Story is the story the change says it implements. Without one, the
	// review is told there are no acceptance criteria.
	Story *seededStory `json:"story,omitempty"`
	// Unmet names the criteria, AC1 and so on, that the change leaves out:
	// the review must judge them not_satisfied, and every other criterion
	// satisfied.
	Unmet []string `json:"unmet,omitempty"`
	// OutsidePath is a file where the change does something the story does
	// not ask for: the review must list a change there as outside the story.
	// Without it, the review must list nothing outside the story.
	OutsidePath string `json:"outside_path,omitempty"`
}

// seededStory is a story as Jira would give it, criteria already read out.
type seededStory struct {
	Key         string   `json:"key"`
	Summary     string   `json:"summary"`
	Description string   `json:"description"`
	Criteria    []string `json:"criteria"`
}

// story is the seeded story as prepare would write it, or none.
func (c seededCase) story() story {
	if c.Story == nil {
		return unavailableStory("", "no story key such as KAN-21 starts the pull request's title or first commit message, or a part of its branch name")
	}
	return story{
		Key:         c.Story.Key,
		Summary:     c.Story.Summary,
		Description: c.Story.Description,
		Criteria:    numberCriteria(c.Story.Criteria),
	}
}

func loadCase(dir string) (seededCase, error) {
	var c seededCase
	raw, err := os.ReadFile(filepath.Join(dir, caseFile))
	if err != nil {
		return c, fmt.Errorf("read the seeded case: %w", err)
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		return c, fmt.Errorf("decode %s: %w", filepath.Join(dir, caseFile), err)
	}
	c.Name = filepath.Base(dir)
	return c, c.validate()
}

func (c seededCase) validate() error {
	switch {
	case strings.TrimSpace(c.Title) == "" || strings.TrimSpace(c.Description) == "":
		return fmt.Errorf("seeded case %s needs a title and a description", c.Name)
	case c.Path == "" && (c.From != 0 || c.To != 0):
		return fmt.Errorf("seeded case %s gives lines but no path", c.Name)
	case c.Path != "" && (c.From < 1 || c.To < c.From):
		return fmt.Errorf("seeded case %s needs lines from 1 up, with to no less than from", c.Name)
	case c.Breaks == "" && (c.Path != "" || c.CitesCoverage):
		return fmt.Errorf("seeded case %s breaks no rule, so it can expect no comment", c.Name)
	case c.MinSeverity != "" && c.Path == "":
		return fmt.Errorf("seeded case %s asks for a severity but names no lines for the comment", c.Name)
	case c.MinSeverity != "" && (c.MinSeverity == severityConcern || severityRank[c.MinSeverity] == 0):
		return fmt.Errorf("seeded case %s asks for severity %q, not critical, high, medium or low", c.Name, c.MinSeverity)
	}
	return c.validateStory()
}

func (c seededCase) validateStory() error {
	if c.Story == nil {
		if len(c.Unmet) > 0 || c.OutsidePath != "" {
			return fmt.Errorf("seeded case %s expects verdicts on a story but has none", c.Name)
		}
		return nil
	}
	if strings.TrimSpace(c.Story.Key) == "" || strings.TrimSpace(c.Story.Summary) == "" || len(c.Story.Criteria) == 0 {
		return fmt.Errorf("seeded case %s needs a story key, summary and at least one criterion", c.Name)
	}
	known := map[string]bool{}
	for _, id := range c.story().criterionIDs() {
		known[id] = true
	}
	for _, id := range c.Unmet {
		if !known[id] {
			return fmt.Errorf("seeded case %s expects %s unmet, which its story does not have", c.Name, id)
		}
	}
	return nil
}

// casesCommand lists the seeded cases in dir as the JSON step output
// "cases", for a matrix of jobs.
func casesCommand(dir, outputs string, out io.Writer) error {
	names, err := caseNames(dir)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(names)
	if err != nil {
		return fmt.Errorf("encode the seeded cases: %w", err)
	}
	fmt.Fprintf(out, "%d seeded cases: %s\n", len(names), strings.Join(names, ", "))
	return appendFile(outputs, fmt.Sprintf("cases=%s\n", raw))
}

// caseNames lists the directories of dir that hold a seeded case.
func caseNames(dir string) ([]string, error) {
	if dir == "" {
		return nil, errors.New("missing environment: SEEDED_DIR")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("list the seeded cases: %w", err)
	}
	var names []string
	for _, entry := range entries {
		if _, err := os.Stat(filepath.Join(dir, entry.Name(), caseFile)); entry.IsDir() && err == nil {
			names = append(names, entry.Name())
		}
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("no seeded cases in %s", dir)
	}
	sort.Strings(names)
	return names, nil
}

// seedCommand writes a seeded case into reviewDir as prepare writes a pull
// request, so the review reads it the same way. The workflow applies the
// case's change to the checkout first, so the files Gemini reads match it.
func seedCommand(caseDir, reviewDir, repo string, out io.Writer) error {
	if caseDir == "" {
		return errors.New("missing environment: SEEDED_CASE")
	}
	c, err := loadCase(caseDir)
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(filepath.Join(caseDir, caseChangeFile))
	if err != nil {
		return fmt.Errorf("read the seeded change: %w", err)
	}
	files, err := parseDiff(string(raw))
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return fmt.Errorf("seeded case %s changes no file", c.Name)
	}

	if err := os.MkdirAll(reviewDir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", reviewDir, err)
	}
	if repo == "" {
		repo = "this repository"
	}
	// The case's name and expectations stay out of what Gemini reads, so
	// the review cannot be told what to find. evaluate reads them from the
	// case itself, which the workflow keeps outside the checkout.
	pr := pullRequest{Title: c.Title, Body: c.Description}
	written := map[string]string{
		pullRequestFile: pullRequestDocument(repo, pr, files),
		rawDiffFile:     string(raw),
		headFile:        "seeded",
	}
	for name, content := range written {
		if err := os.WriteFile(filepath.Join(reviewDir, name), []byte(content), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", name, err)
		}
	}
	if err := writeStory(reviewDir, c.story()); err != nil {
		return err
	}
	fmt.Fprintf(out, "Seeded case %s: %d files changed. %s\n", c.Name, len(files), storyStatus(c.story()))
	return nil
}

// evaluateInputs are what evaluate needs besides the review directory.
type evaluateInputs struct {
	reply         string // the model's answer
	cliOutput     string // Gemini CLI's JSON output file
	standardsFile string // GEMINI.md
	summary       string // the job summary file; empty outside Actions
}

// evaluateCommand checks the review of a seeded case against what the case
// expects, and fails when the review missed the defect or flagged a clean
// change. It posts nothing.
func evaluateCommand(caseDir, reviewDir string, inputs evaluateInputs, out io.Writer) error {
	if caseDir == "" {
		return errors.New("missing environment: SEEDED_CASE")
	}
	c, err := loadCase(caseDir)
	if err != nil {
		return err
	}
	var computed facts
	if err := readJSON(filepath.Join(reviewDir, factsDataFile), &computed); err != nil {
		return err
	}
	decision, err := readRouting(reviewDir)
	if err != nil {
		return err
	}
	reply, err := parseGeminiReview(inputs.reply)
	if err != nil {
		return err
	}
	review, err := assembleFromDir(reply, inputs.standardsFile, reviewDir)
	if err != nil {
		return err
	}
	if err := confirmModel(inputs.cliOutput, decision.Model); err != nil {
		return err
	}

	misses := c.misses(review, computed.InternalCoverage)
	report := evaluationReport(c, review, decision, misses)
	fmt.Fprint(out, report)
	if err := appendFile(inputs.summary, report); err != nil {
		return err
	}
	if len(misses) > 0 {
		return fmt.Errorf("the review of seeded case %s missed what it must find: %s", c.Name, strings.Join(misses, "; "))
	}
	return nil
}

// misses lists what the review should have said about the case and did not.
func (c seededCase) misses(review assembledReview, coverage string) []string {
	return append(c.ruleMisses(review, coverage), c.storyMisses(review.Story)...)
}

// ruleMisses lists what the review should have said about the rule the case
// breaks, or about a change that breaks none, and did not.
func (c seededCase) ruleMisses(review assembledReview, coverage string) []string {
	if c.Breaks == "" {
		return cleanMisses(review)
	}

	var misses []string
	verdict := review.verdictFor(c.Breaks)
	if verdict.Verdict != verdictFail {
		misses = append(misses, fmt.Sprintf("the review did not fail %s (verdict %q)", c.Breaks, verdict.Verdict))
	}
	if c.Path != "" && !c.flaggedOnLine(review.Comments) {
		misses = append(misses, fmt.Sprintf("no confirmed comment%s on %s lines %d to %d", c.severityWanted(), c.Path, c.From, c.To))
	}
	if c.CitesCoverage && (coverage == "" || !strings.Contains(verdict.Reason, strings.TrimSuffix(coverage, "%"))) {
		misses = append(misses, fmt.Sprintf("the %s verdict does not cite the computed coverage, %s", c.Breaks, coverage))
	}
	return misses
}

// storyMisses lists what the review should have said about the change
// against its story and did not: each criterion the change leaves out
// judged not_satisfied, every other one satisfied, and a change outside the
// story listed where there is one, and none where there is not.
func (c seededCase) storyMisses(review storyReview) []string {
	if c.Story == nil {
		return nil
	}
	var misses []string
	for _, id := range review.Story.criterionIDs() {
		want := criterionSatisfied
		if slices.Contains(c.Unmet, id) {
			want = criterionNotSatisfied
		}
		if got := review.verdictFor(id); got != want {
			misses = append(misses, fmt.Sprintf("the review judged %s %q, want %q", id, got, want))
		}
	}
	switch {
	case c.OutsidePath != "" && !listsOutside(review.Outside, c.OutsidePath):
		misses = append(misses, "the review lists no change in "+c.OutsidePath+" as outside the story")
	case c.OutsidePath == "" && len(review.Outside) > 0:
		misses = append(misses, fmt.Sprintf("every change serves the story, but the review lists %d as outside it", len(review.Outside)))
	}
	return misses
}

func listsOutside(changes []outsideChange, path string) bool {
	for _, change := range changes {
		if change.Path == path {
			return true
		}
	}
	return false
}

// cleanMisses lists what the review said about a clean change that it
// should not have: any comment, a concern included, and any failed rule or
// concern.
func cleanMisses(review assembledReview) []string {
	var misses []string
	if len(review.Comments) > 0 {
		misses = append(misses, fmt.Sprintf("the change is clean, but the review made %d comments", len(review.Comments)))
	}
	if failed := rulesWithVerdict(review.Checklist, verdictFail); len(failed) > 0 {
		misses = append(misses, "the change is clean, but the review failed "+strings.Join(failed, ", "))
	}
	if doubted := rulesWithVerdict(review.Checklist, verdictConcern); len(doubted) > 0 {
		misses = append(misses, "the change is clean, but the review raised a concern on "+strings.Join(doubted, ", "))
	}
	return misses
}

// flaggedOnLine reports whether a confirmed comment, at least as severe as
// the case asks, sits on the case's path, on the pull request's side,
// between its lines.
func (c seededCase) flaggedOnLine(comments []geminiComment) bool {
	for _, comment := range comments {
		side := strings.ToUpper(strings.TrimSpace(comment.Side))
		if comment.Path == c.Path && side != sideLeft && comment.Line >= c.From && comment.Line <= c.To &&
			comment.atLeast(c.MinSeverity) {
			return true
		}
	}
	return false
}

// severityWanted names the least severity the case asks for, if any, as
// the evaluation reports it.
func (c seededCase) severityWanted() string {
	if c.MinSeverity == "" {
		return ""
	}
	return " of " + c.MinSeverity + " or higher severity"
}

// evaluationReport is the job summary of one seeded case's review.
func evaluationReport(c seededCase, review assembledReview, decision routing, misses []string) string {
	var b strings.Builder
	outcome := "✅ caught"
	if c.Breaks == "" {
		outcome = "✅ left alone"
	}
	if len(misses) > 0 {
		outcome = "❌ missed"
	}
	expected := "no comment, no failed rule and no concern"
	if c.Breaks != "" {
		expected = "a failed `" + c.Breaks + "`"
		if c.Path != "" {
			expected += fmt.Sprintf(" and a confirmed comment%s on `%s` lines %d to %d", c.severityWanted(), c.Path, c.From, c.To)
		}
	}
	expected += c.storyExpected()
	fmt.Fprintf(&b, "### Seeded case `%s`: %s\n\n", c.Name, outcome)
	fmt.Fprintf(&b, "Expected %s. %s\n\n", expected, decision.explain())
	for _, miss := range misses {
		fmt.Fprintf(&b, "- Missed: %s\n", miss)
	}
	if len(misses) > 0 {
		b.WriteString("\n")
	}
	b.WriteString("#### Comments\n\n")
	if len(review.Comments) == 0 {
		b.WriteString("None.\n")
	}
	for _, comment := range review.Comments {
		fmt.Fprintf(&b, "- `%s` line %d: %s\n", comment.Path, comment.Line, tableCell(withSeverity(comment)))
	}
	b.WriteString("\n" + storySection(review.Story))
	b.WriteString("\n" + checklistTable(review.Checklist) + "\n")
	return b.String()
}

// storyExpected says, for the evaluation report, what the review must say
// about the case's story.
func (c seededCase) storyExpected() string {
	if c.Story == nil {
		return ""
	}
	expected := "; every acceptance criterion satisfied"
	if len(c.Unmet) > 0 {
		expected = "; " + strings.Join(c.Unmet, ", ") + " not satisfied and every other criterion satisfied"
	}
	if c.OutsidePath != "" {
		return expected + "; a change in `" + c.OutsidePath + "` listed as outside the story"
	}
	return expected + "; nothing outside the story"
}

func readJSON(name string, target any) error {
	raw, err := os.ReadFile(name)
	if err != nil {
		return fmt.Errorf("read %s: %w", filepath.Base(name), err)
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return fmt.Errorf("decode %s: %w", filepath.Base(name), err)
	}
	return nil
}
