package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
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
// when Path is set, comment on Path between lines From and To. A case that
// breaks nothing must get no comment and no failed rule.
type seededCase struct {
	Name        string `json:"name"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Breaks      string `json:"breaks"`
	Path        string `json:"path,omitempty"`
	From        int    `json:"from,omitempty"`
	To          int    `json:"to,omitempty"`
	// CitesCoverage asks that the verdict's reason give the computed
	// coverage of internal/ from the facts.
	CitesCoverage bool `json:"cites_coverage,omitempty"`
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
	fmt.Fprintf(out, "Seeded case %s: %d files changed.\n", c.Name, len(files))
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
	if c.Breaks == "" {
		var misses []string
		if len(review.Comments) > 0 {
			misses = append(misses, fmt.Sprintf("the change is clean, but the review made %d comments", len(review.Comments)))
		}
		if failed := failedRules(review.Checklist); len(failed) > 0 {
			misses = append(misses, "the change is clean, but the review failed "+strings.Join(failed, ", "))
		}
		return misses
	}

	var misses []string
	verdict := review.verdictFor(c.Breaks)
	if verdict.Verdict != verdictFail {
		misses = append(misses, fmt.Sprintf("the review did not fail %s (verdict %q)", c.Breaks, verdict.Verdict))
	}
	if c.Path != "" && !c.flaggedOnLine(review.Comments) {
		misses = append(misses, fmt.Sprintf("no comment on %s lines %d to %d", c.Path, c.From, c.To))
	}
	if c.CitesCoverage && (coverage == "" || !strings.Contains(verdict.Reason, strings.TrimSuffix(coverage, "%"))) {
		misses = append(misses, fmt.Sprintf("the %s verdict does not cite the computed coverage, %s", c.Breaks, coverage))
	}
	return misses
}

// flaggedOnLine reports whether a comment sits on the case's path, on the
// pull request's side, between its lines.
func (c seededCase) flaggedOnLine(comments []geminiComment) bool {
	for _, comment := range comments {
		side := strings.ToUpper(strings.TrimSpace(comment.Side))
		if comment.Path == c.Path && side != sideLeft && comment.Line >= c.From && comment.Line <= c.To {
			return true
		}
	}
	return false
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
	expected := "no comment and no failed rule"
	if c.Breaks != "" {
		expected = "a failed `" + c.Breaks + "`"
		if c.Path != "" {
			expected += fmt.Sprintf(" and a comment on `%s` lines %d to %d", c.Path, c.From, c.To)
		}
	}
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
	b.WriteString("\n" + checklistTable(review.Checklist) + "\n")
	return b.String()
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
