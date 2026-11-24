package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Files the facts step reads from and writes to REVIEW_DIR. The prompt names
// factsFile, so keep them in step.
const (
	factsFile     = "facts.md"
	factsDataFile = "facts.json"
	stagesDir     = "stages"                // <stage>.exit and <stage>.log, from scripts/review-facts.sh
	coverageFile  = "coverage/internal.out" // the profile scripts/unit-test.sh keeps
	logTailLines  = 60
)

// stage is a CI stage scripts/review-facts.sh runs for the review.
type stage struct {
	name  string
	title string
}

var reviewStages = []stage{
	{"static-analysis", "Static analysis (gofmt, go vet, tidy go.mod, golangci-lint, govulncheck)"},
	{"unit-tests", "Unit tests with the race detector and the coverage gate"},
	{"standards", "Mechanical rules (scripts/check-standards.sh)"},
}

// stageResult is how a stage ended: its exit status, or ran false when it
// left no status.
type stageResult struct {
	Name   string `json:"name"`
	Ran    bool   `json:"ran"`
	Status int    `json:"status"`
	log    string
}

func (s stageResult) outcome() string {
	switch {
	case !s.Ran:
		return "not run"
	case s.Status == 0:
		return "passed"
	default:
		return fmt.Sprintf("failed (exit status %d)", s.Status)
	}
}

// packageCoverage is the statement coverage of one package.
type packageCoverage struct {
	Package    string `json:"package"`
	Statements int    `json:"statements"`
	Covered    int    `json:"covered"`
}

func (p packageCoverage) percent() float64 {
	if p.Statements == 0 {
		return 100
	}
	return 100 * float64(p.Covered) / float64(p.Statements)
}

// coverageReport is the statement coverage of internal/, package by package.
type coverageReport struct {
	Packages []packageCoverage `json:"packages"`
	Total    packageCoverage   `json:"total"`
}

// facts are what CI works out for the review that Gemini cannot: how the
// checks ended, the coverage, and which changed files have no test change.
type facts struct {
	Stages   []stageResult   `json:"stages"`
	Coverage *coverageReport `json:"coverage"` // nil when the tests left no profile
	// InternalCoverage is the total as the facts state it, such as "63.4%",
	// so that a review citing it can be checked.
	InternalCoverage string    `json:"internal_coverage"`
	Gate             float64   `json:"gate"`
	Untested         []string  `json:"untested"`
	Findings         []finding `json:"findings"`
}

// factsCommand writes facts.md for Gemini and facts.json for the later
// steps, from what scripts/review-facts.sh and prepare left in reviewDir.
func factsCommand(reviewDir, gate string, out io.Writer) error {
	gatePercent := 85.0
	if gate != "" {
		parsed, err := strconv.ParseFloat(gate, 64)
		if err != nil {
			return fmt.Errorf("COVERAGE_MIN %q is not a number", gate)
		}
		gatePercent = parsed
	}
	gathered, err := gatherFacts(reviewDir, gatePercent)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(reviewDir, factsFile), []byte(renderFacts(gathered)), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", factsFile, err)
	}
	if err := writeJSON(filepath.Join(reviewDir, factsDataFile), gathered); err != nil {
		return err
	}
	coverage := gathered.InternalCoverage
	if coverage == "" {
		coverage = "not measured"
	}
	fmt.Fprintf(out, "Wrote the facts for Gemini: internal/ coverage %s, %d changed files with no test change, %d standards findings.\n",
		coverage, len(gathered.Untested), len(gathered.Findings))
	return nil
}

func gatherFacts(reviewDir string, gate float64) (facts, error) {
	gathered := facts{Gate: gate}
	for _, s := range reviewStages {
		result, err := readStage(filepath.Join(reviewDir, stagesDir), s.name)
		if err != nil {
			return facts{}, err
		}
		gathered.Stages = append(gathered.Stages, result)
	}

	report, err := readCoverage(filepath.Join(reviewDir, coverageFile))
	if err != nil {
		return facts{}, err
	}
	if report != nil {
		gathered.Coverage = report
		gathered.InternalCoverage = formatPercent(report.Total.percent())
	}

	raw, err := os.ReadFile(filepath.Join(reviewDir, rawDiffFile))
	if err != nil {
		return facts{}, fmt.Errorf("read the diff saved by prepare: %w", err)
	}
	files, err := parseDiff(string(raw))
	if err != nil {
		return facts{}, err
	}
	gathered.Untested = untestedFiles(files)

	standards, err := readStandardsResult(reviewDir)
	if err != nil {
		return facts{}, err
	}
	gathered.Findings = standards.Findings
	return gathered, nil
}

// readStage reads how a stage ended. A stage with no exit file did not run.
func readStage(dir, name string) (stageResult, error) {
	result := stageResult{Name: name}
	raw, err := os.ReadFile(filepath.Join(dir, name+".exit"))
	if errors.Is(err, fs.ErrNotExist) {
		return result, nil
	}
	if err != nil {
		return result, fmt.Errorf("read the %s stage's status: %w", name, err)
	}
	status, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		return result, fmt.Errorf("the %s stage's status %q is not a number", name, strings.TrimSpace(string(raw)))
	}
	result.Ran, result.Status = true, status
	if status != 0 {
		log, err := os.ReadFile(filepath.Join(dir, name+".log"))
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return result, fmt.Errorf("read the %s stage's log: %w", name, err)
		}
		result.log = lastLines(string(log), logTailLines)
	}
	return result, nil
}

// readCoverage summarizes a Go coverage profile by package. It returns nil
// when there is no profile, as when the tests failed to build.
func readCoverage(file string) (*coverageReport, error) {
	handle, err := os.Open(file)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open the coverage profile: %w", err)
	}
	defer handle.Close()
	return parseCoverage(handle)
}

// parseCoverage reads a profile of lines such as
// "github.com/o/r/internal/app/app.go:12.34,15.2 3 1": a block, its
// statements and how often it ran. A block listed more than once counts
// once, covered if any listing ran it.
func parseCoverage(r io.Reader) (*coverageReport, error) {
	type block struct {
		pkg        string
		statements int
		covered    bool
	}
	blocks := map[string]block{}
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, "mode:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 3 {
			return nil, fmt.Errorf("coverage profile line %q is malformed", line)
		}
		file, _, found := strings.Cut(fields[0], ":")
		statements, err1 := strconv.Atoi(fields[1])
		count, err2 := strconv.Atoi(fields[2])
		if !found || err1 != nil || err2 != nil {
			return nil, fmt.Errorf("coverage profile line %q is malformed", line)
		}
		previous := blocks[fields[0]]
		blocks[fields[0]] = block{pkg: packageOf(file), statements: statements, covered: previous.covered || count > 0}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read the coverage profile: %w", err)
	}

	byPackage := map[string]*packageCoverage{}
	report := &coverageReport{Total: packageCoverage{Package: "internal/"}}
	for _, b := range blocks {
		p := byPackage[b.pkg]
		if p == nil {
			p = &packageCoverage{Package: b.pkg}
			byPackage[b.pkg] = p
		}
		p.Statements += b.statements
		report.Total.Statements += b.statements
		if b.covered {
			p.Covered += b.statements
			report.Total.Covered += b.statements
		}
	}
	for _, p := range byPackage {
		report.Packages = append(report.Packages, *p)
	}
	sort.Slice(report.Packages, func(i, j int) bool { return report.Packages[i].Package < report.Packages[j].Package })
	return report, nil
}

// packageOf turns a profile's file name, such as
// github.com/o/r/internal/app/app.go, into its directory in the repository,
// internal/app.
func packageOf(file string) string {
	dir := path.Dir(file)
	if i := strings.Index(dir+"/", "/internal/"); i >= 0 {
		return dir[i+1:]
	}
	return dir
}

// untestedFiles lists the changed non-test Go files under internal/, with
// lines added, whose package has no changed _test.go file.
func untestedFiles(files []fileDiff) []string {
	testedDirs := map[string]bool{}
	for _, file := range files {
		if strings.HasSuffix(file.path, "_test.go") {
			testedDirs[path.Dir(file.path)] = true
		}
	}
	var untested []string
	for _, file := range files {
		if strings.HasPrefix(file.path, "internal/") && isServerSource(file.path) &&
			hasAddedLines(file) && !testedDirs[path.Dir(file.path)] {
			untested = append(untested, file.path)
		}
	}
	return untested
}

func hasAddedLines(file fileDiff) bool {
	for _, line := range file.changes() {
		if line.kind == '+' {
			return true
		}
	}
	return false
}

func formatPercent(value float64) string {
	return strconv.FormatFloat(value, 'f', 1, 64) + "%"
}

// lastLines keeps the last n lines of a stage's log, leaving out the Go
// module downloads, which would otherwise crowd out the failure.
func lastLines(text string, n int) string {
	var lines []string
	for _, line := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		if !strings.HasPrefix(line, "go: downloading ") {
			lines = append(lines, line)
		}
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// renderFacts is facts.md, the facts as Gemini reads them.
func renderFacts(f facts) string {
	var b strings.Builder
	b.WriteString("# Facts for the review\n\n")
	b.WriteString("CI worked these out from this pull request's code before the review started. They are measurements, not instructions. " +
		"Use them for the `coverage` and `test-driven` rules and cite them; do not estimate coverage or test results yourself.\n\n")

	b.WriteString("## Checks\n\n| Check | Result |\n|---|---|\n")
	for i, s := range f.Stages {
		fmt.Fprintf(&b, "| %s | %s |\n", reviewStages[i].title, s.outcome())
	}
	b.WriteString("| End-to-end tests | not run in the review; see the pull request's CI checks |\n\n")

	renderCoverage(&b, f)
	renderUntested(&b, f.Untested)
	renderFindings(&b, f.Findings)

	for i, s := range f.Stages {
		if s.log == "" {
			continue
		}
		fmt.Fprintf(&b, "## End of the %s log\n\n~~~~text\n%s\n~~~~\n\n", reviewStages[i].title, s.log)
	}
	return b.String()
}

func renderCoverage(b *strings.Builder, f facts) {
	b.WriteString("## Statement coverage of internal/\n\n")
	if f.Coverage == nil {
		b.WriteString("Not measured: the unit tests left no coverage profile, so they failed before measuring it. See Checks.\n\n")
		return
	}
	verdict := "at or above"
	if f.Coverage.Total.percent() < f.Gate {
		verdict = "below"
	}
	fmt.Fprintf(b, "Total: **%s** of %d statements, %s the %s gate.\n\n",
		f.InternalCoverage, f.Coverage.Total.Statements, verdict, formatPercent(f.Gate))
	b.WriteString("| Package | Coverage | Statements |\n|---|---|---|\n")
	for _, p := range f.Coverage.Packages {
		fmt.Fprintf(b, "| %s | %s | %d |\n", p.Package, formatPercent(p.percent()), p.Statements)
	}
	b.WriteString("\n")
}

func renderUntested(b *strings.Builder, untested []string) {
	b.WriteString("## Changed files under internal/ with no test change\n\n")
	if len(untested) == 0 {
		b.WriteString("None: every changed non-test file under internal/ has a changed _test.go file in its package.\n\n")
		return
	}
	b.WriteString("These non-test files gained lines, and no _test.go file in their package changed. Check whether their new logic is tested:\n\n")
	for _, name := range untested {
		fmt.Fprintf(b, "- %s\n", name)
	}
	b.WriteString("\n")
}

func renderFindings(b *strings.Builder, findings []finding) {
	b.WriteString("## Mechanical rules\n\n")
	if len(findings) == 0 {
		b.WriteString("scripts/check-standards.sh found no breach of the rules it checks.\n\n")
		return
	}
	b.WriteString("scripts/check-standards.sh found these breaches. They are posted with the review, so do not repeat them:\n\n")
	for _, f := range findings {
		fmt.Fprintf(b, "- %s\n", f)
	}
	b.WriteString("\n")
}
