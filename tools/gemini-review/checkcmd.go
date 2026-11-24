package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// standardsResult is what the standards command records for the review:
// the rules it checked and what it found.
type standardsResult struct {
	Rules    []string  `json:"rules"`
	Findings []finding `json:"findings"`
}

// standardsResultFile is where the review keeps the standards check's
// result in REVIEW_DIR.
const standardsResultFile = "standards.json"

// standardsCommand checks the mechanical rules of GEMINI.md against the
// repository at root and the change in diffFile. It prints each breach,
// naming its rule, writes the result to resultFile when one is given, and
// fails when it finds any.
func standardsCommand(root, diffFile, resultFile string, out io.Writer) error {
	if diffFile == "" {
		return errors.New("missing environment: DIFF_FILE")
	}
	raw, err := os.ReadFile(diffFile)
	if err != nil {
		return fmt.Errorf("read the diff to check: %w", err)
	}
	files, err := parseDiff(string(raw))
	if err != nil {
		return err
	}
	findings, err := checkStandards(root, files)
	if err != nil {
		return err
	}
	if resultFile != "" {
		if err := writeJSON(resultFile, standardsResult{Rules: checkedRules, Findings: findings}); err != nil {
			return err
		}
	}
	for _, f := range findings {
		fmt.Fprintf(out, "FAIL %s\n", f)
	}
	if len(findings) > 0 {
		return fmt.Errorf("%d breaches of the mechanical rules in GEMINI.md", len(findings))
	}
	fmt.Fprintf(out, "The mechanical rules of GEMINI.md hold: %v\n", checkedRules)
	return nil
}

// readStandardsResult reads the result the standards command saved in dir.
func readStandardsResult(dir string) (standardsResult, error) {
	var result standardsResult
	if err := readJSON(filepath.Join(dir, standardsResultFile), &result); err != nil {
		return result, fmt.Errorf("the standards check's result: %w", err)
	}
	return result, nil
}

func writeJSON(name string, value any) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", filepath.Base(name), err)
	}
	if err := os.WriteFile(name, raw, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", name, err)
	}
	return nil
}
