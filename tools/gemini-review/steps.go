package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// stepConfig is what route and decide need: the shared directory, and the
// files Actions reads step outputs and the job summary from.
type stepConfig struct {
	reviewDir string
	outputs   string // GITHUB_OUTPUT; empty outside Actions
	summary   string // GITHUB_STEP_SUMMARY; empty outside Actions
}

func loadStepConfig(getenv func(string) string) (stepConfig, error) {
	if getenv("REVIEW_DIR") == "" {
		return stepConfig{}, errors.New("missing environment: REVIEW_DIR")
	}
	return stepConfig{
		reviewDir: getenv("REVIEW_DIR"),
		outputs:   getenv("GITHUB_OUTPUT"),
		summary:   getenv("GITHUB_STEP_SUMMARY"),
	}, nil
}

// routeCommand applies the routing file's rules to the diff prepare saved.
func routeCommand(steps stepConfig, configFile string, out io.Writer) error {
	if configFile == "" {
		return errors.New("missing environment: ROUTING_CONFIG")
	}
	cfg, err := loadRoutingConfig(configFile)
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(filepath.Join(steps.reviewDir, rawDiffFile))
	if err != nil {
		return fmt.Errorf("read the diff saved by prepare: %w", err)
	}
	files, err := parseDiff(string(raw))
	if err != nil {
		return err
	}

	decision := assess(files, cfg)
	if err := writeRouting(steps.reviewDir, decision); err != nil {
		return err
	}
	if decision.needsTriage() {
		fmt.Fprintf(out, "No rule makes this pull request high-risk; %s triages it next.\n", decision.TriageModel)
	} else {
		fmt.Fprintf(out, "%s\n", decision.explain())
	}
	return appendFile(steps.outputs, fmt.Sprintf("needs_triage=%t\ntriage_model=%s\n", decision.needsTriage(), decision.TriageModel))
}

// decideCommand completes the routing with the triage model's verdict, when
// triage ran, and names the model that reviews.
func decideCommand(steps stepConfig, triageReply, triageOutput string, out io.Writer) error {
	decision, err := readRouting(steps.reviewDir)
	if err != nil {
		return err
	}
	if decision.needsTriage() {
		verdict, err := parseTriageVerdict(triageReply)
		if err != nil {
			return err
		}
		if err := confirmModel(triageOutput, decision.TriageModel); err != nil {
			return fmt.Errorf("triage: %w", err)
		}
		decision = decision.withTriage(verdict)
		if err := writeRouting(steps.reviewDir, decision); err != nil {
			return err
		}
	}

	fmt.Fprintf(out, "%s\n", decision.explain())
	if err := appendFile(steps.summary, routingSummary(decision)); err != nil {
		return err
	}
	return appendFile(steps.outputs, fmt.Sprintf("model=%s\ntier=%s\n", decision.Model, decision.Tier))
}

// routingSummary is the job summary's account of the routing.
func routingSummary(r routing) string {
	var b strings.Builder
	b.WriteString("### Gemini review routing\n\n")
	b.WriteString(r.explain() + "\n\n")
	fmt.Fprintf(&b, "- Changed lines: %d (high-risk from %d)\n", r.ChangedLines, r.Threshold)
	fmt.Fprintf(&b, "- Critical paths touched: %d\n", len(r.CriticalHits))
	fmt.Fprintf(&b, "- Exported API changes in shared packages: %d\n", len(r.APIChanges))
	if r.Triage != nil {
		fmt.Fprintf(&b, "- Triage by `%s`: %s\n", r.TriageModel, strings.TrimSpace(r.Triage.Summary))
	} else {
		b.WriteString("- Triage: skipped, because a rule already makes this pull request high-risk\n")
	}
	b.WriteString("\n")
	return b.String()
}

func writeRouting(dir string, r routing) error {
	raw, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("encode the routing: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, routingFile), raw, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", routingFile, err)
	}
	return nil
}

func readRouting(dir string) (routing, error) {
	var r routing
	raw, err := os.ReadFile(filepath.Join(dir, routingFile))
	if err != nil {
		return r, fmt.Errorf("read the routing saved by route: %w", err)
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return r, fmt.Errorf("decode %s: %w", routingFile, err)
	}
	return r, nil
}

// appendFile adds text to a file Actions reads after the step, such as
// GITHUB_OUTPUT. Outside Actions, name is empty and nothing is written.
func appendFile(name, text string) error {
	if name == "" {
		return nil
	}
	file, err := os.OpenFile(name, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open %s: %w", name, err)
	}
	_, writeErr := io.WriteString(file, text)
	closeErr := file.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return fmt.Errorf("write %s: %w", name, err)
	}
	return nil
}
