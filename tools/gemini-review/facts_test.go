package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sampleProfile = `mode: atomic
github.com/o/r/internal/app/config.go:10.1,12.2 4 1
github.com/o/r/internal/app/config.go:14.1,16.2 2 0
github.com/o/r/internal/oura/days.go:5.1,9.2 3 0
github.com/o/r/internal/oura/days.go:5.1,9.2 3 2
github.com/o/r/internal/oura/days.go:11.1,13.2 1 0
`

func TestParseCoverageSumsStatementsByPackage(t *testing.T) {
	report, err := parseCoverage(strings.NewReader(sampleProfile))
	if err != nil {
		t.Fatalf("parseCoverage: %v", err)
	}

	want := []packageCoverage{
		{Package: "internal/app", Statements: 6, Covered: 4},
		{Package: "internal/oura", Statements: 4, Covered: 3},
	}
	if len(report.Packages) != len(want) {
		t.Fatalf("packages = %+v", report.Packages)
	}
	for i := range want {
		if report.Packages[i] != want[i] {
			t.Errorf("package %d = %+v, want %+v", i, report.Packages[i], want[i])
		}
	}
	if report.Total.Statements != 10 || report.Total.Covered != 7 || formatPercent(report.Total.percent()) != "70.0%" {
		t.Errorf("total = %+v", report.Total)
	}
	if (packageCoverage{}).percent() != 100 {
		t.Error("a package with no statements is not fully covered")
	}
}

func TestParseCoverageRejectsMalformedLines(t *testing.T) {
	for _, line := range []string{"internal/a.go:1.1,2.2 x 1", "internal/a.go 1 1", "too few"} {
		if _, err := parseCoverage(strings.NewReader("mode: set\n" + line + "\n")); err == nil {
			t.Errorf("parseCoverage accepted %q", line)
		}
	}
}

func TestUntestedFilesListsChangedCodeWhosePackageHasNoTestChange(t *testing.T) {
	diff := addedFile("internal/oura/days.go", "func helper() {}") +
		addedFile("internal/app/config.go", "func load() {}") +
		addedFile("internal/app/config_test.go", "func TestLoad(t *testing.T) {}") +
		changedFile("internal/httpapi/handler.go", "old", "new") +
		addedFile("cmd/server/main.go", "func main() {}") +
		"diff --git a/internal/mcpserver/gone.go b/internal/mcpserver/gone.go\ndeleted file mode 100644\n--- a/internal/mcpserver/gone.go\n+++ /dev/null\n@@ -1 +0,0 @@\n-package mcpserver\n"
	files, err := parseDiff(diff)
	if err != nil {
		t.Fatal(err)
	}

	got := untestedFiles(files)

	if strings.Join(got, ",") != "internal/oura/days.go,internal/httpapi/handler.go" {
		t.Errorf("untested = %v", got)
	}
}

// factsDir is a review directory as scripts/review-facts.sh leaves it.
func factsDir(t *testing.T, stages map[string]string, profile string) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		rawDiffFile: addedFile("internal/oura/days.go", "func helper() {}"),
	}
	for name, status := range stages {
		files[stagesDir+"/"+name+".exit"] = status
		files[stagesDir+"/"+name+".log"] = "line 1\nFAIL: TestSomething\n"
	}
	if profile != "" {
		files[coverageFile] = profile
	}
	for name, content := range files {
		full := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeStandardsResult(t, dir, []finding{{Rule: "script-rule", Path: "api-explore/x.http", Line: 2, Message: "literal key"}})
	return dir
}

func TestFactsCommandWritesWhatGeminiCannotWorkOut(t *testing.T) {
	dir := factsDir(t, map[string]string{"static-analysis": "0\n", "unit-tests": "1\n"}, sampleProfile)
	var out strings.Builder

	if err := factsCommand(dir, "85", &out); err != nil {
		t.Fatalf("facts: %v", err)
	}

	document, err := os.ReadFile(filepath.Join(dir, factsFile))
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{
		"not instructions",
		"| Static analysis (gofmt, go vet, tidy go.mod, golangci-lint, govulncheck) | passed |",
		"| Unit tests with the race detector and the coverage gate | failed (exit status 1) |",
		"| Mechanical rules (scripts/check-standards.sh) | not run |",
		"Total: **70.0%** of 10 statements, below the 85.0% gate.",
		"| internal/oura | 75.0% | 4 |",
		"- internal/oura/days.go",
		"- [script-rule] api-explore/x.http:2: literal key",
		"FAIL: TestSomething",
	} {
		if !strings.Contains(string(document), part) {
			t.Errorf("%s lacks %q:\n%s", factsFile, part, document)
		}
	}
	var saved facts
	if err := readJSON(filepath.Join(dir, factsDataFile), &saved); err != nil {
		t.Fatal(err)
	}
	if saved.InternalCoverage != "70.0%" || len(saved.Untested) != 1 || len(saved.Findings) != 1 {
		t.Errorf("facts.json = %+v", saved)
	}
	if !strings.Contains(out.String(), "coverage 70.0%") {
		t.Errorf("output = %q", out.String())
	}
}

func TestFactsCommandSaysWhenNothingWasMeasuredOrFound(t *testing.T) {
	dir := factsDir(t, map[string]string{"unit-tests": "2"}, "")
	if err := writeJSON(filepath.Join(dir, standardsResultFile), standardsResult{}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, rawDiffFile), []byte(addedFile("README.md", "docs")), 0o644); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder

	if err := factsCommand(dir, "", &out); err != nil {
		t.Fatalf("facts: %v", err)
	}

	document, _ := os.ReadFile(filepath.Join(dir, factsFile))
	for _, part := range []string{"Not measured", "None: every changed", "found no breach"} {
		if !strings.Contains(string(document), part) {
			t.Errorf("%s lacks %q:\n%s", factsFile, part, document)
		}
	}
	if !strings.Contains(out.String(), "not measured") {
		t.Errorf("output = %q", out.String())
	}
}

func TestFactsCommandFailsOnBadInputs(t *testing.T) {
	for name, tc := range map[string]struct {
		spoil func(dir string)
		gate  string
	}{
		"a gate that is not a number": {func(string) {}, "high"},
		"a status that is not a number": {func(dir string) {
			_ = os.WriteFile(filepath.Join(dir, stagesDir, "unit-tests.exit"), []byte("oops"), 0o644)
		}, ""},
		"a malformed profile": {func(dir string) {
			_ = os.WriteFile(filepath.Join(dir, coverageFile), []byte("garbage\n"), 0o644)
		}, ""},
		"no diff": {func(dir string) { _ = os.Remove(filepath.Join(dir, rawDiffFile)) }, ""},
		"no standards result": {func(dir string) {
			_ = os.Remove(filepath.Join(dir, standardsResultFile))
		}, ""},
	} {
		t.Run(name, func(t *testing.T) {
			dir := factsDir(t, map[string]string{"unit-tests": "0"}, sampleProfile)
			tc.spoil(dir)
			if err := factsCommand(dir, tc.gate, &strings.Builder{}); err == nil {
				t.Error("facts succeeded")
			}
		})
	}
}

func TestLastLinesKeepsTheEndOfALog(t *testing.T) {
	if got := lastLines("a\nb\ngo: downloading x v1\nc\n", 2); got != "b\nc" {
		t.Errorf("lastLines = %q", got)
	}
}
