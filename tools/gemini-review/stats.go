package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

// cliOutput is the part of Gemini CLI's --output-format json output that
// says which models answered. run-gemini-cli keeps it as
// gemini-artifacts/stdout.log.
type cliOutput struct {
	Stats struct {
		Models map[string]struct {
			API struct {
				TotalRequests int `json:"totalRequests"`
				TotalErrors   int `json:"totalErrors"`
			} `json:"api"`
		} `json:"models"`
	} `json:"stats"`
}

// confirmModel checks, from Gemini CLI's own statistics, that the routed
// model answered. The CLI can fall back to another model on its own, and a
// review that names a model that did not run would mislead.
func confirmModel(stdoutFile, model string) error {
	raw, err := os.ReadFile(stdoutFile)
	if err != nil {
		return fmt.Errorf("read Gemini CLI's output to confirm the model: %w", err)
	}
	var output cliOutput
	if err := json.Unmarshal(raw, &output); err != nil {
		return fmt.Errorf("gemini CLI's output in %s is not JSON: %w", stdoutFile, err)
	}
	answered := make([]string, 0, len(output.Stats.Models))
	for name, metrics := range output.Stats.Models {
		if metrics.API.TotalRequests > metrics.API.TotalErrors {
			answered = append(answered, name)
		}
	}
	for _, name := range answered {
		if name == model {
			return nil
		}
	}
	sort.Strings(answered)
	return fmt.Errorf("the routed model %s did not answer; Gemini CLI's statistics show answers from [%s]",
		model, strings.Join(answered, ", "))
}
