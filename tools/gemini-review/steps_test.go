package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// routed runs prepare and route on the sample pull request (6 changed lines
// in internal/app/config.go, docs/new.md and logo.png) with the given
// routing file, and returns the review directory and the step files.
func routed(t *testing.T, routingConfig string) (string, map[string]string) {
	t.Helper()
	fake := &fakeGitHub{}
	dir := prepared(t, fake)
	env := map[string]string{
		"ROUTING_CONFIG":      writeFile(t, routingConfig),
		"GITHUB_OUTPUT":       filepath.Join(t.TempDir(), "output"),
		"GITHUB_STEP_SUMMARY": filepath.Join(t.TempDir(), "summary.md"),
	}
	if _, err := runTool(t, fake, dir, env, "route"); err != nil {
		t.Fatalf("route: %v", err)
	}
	return dir, env
}

func readText(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestRouteAsksForTriageWhenNoRuleFires(t *testing.T) {
	_, env := routed(t, testRouting)

	if got := readText(t, env["GITHUB_OUTPUT"]); got != "needs_triage=true\ntriage_model=lite\n" {
		t.Errorf("outputs = %q", got)
	}
}

func TestDecideAppliesTheTriageVerdict(t *testing.T) {
	for name, tc := range map[string]struct {
		reply, model string
	}{
		"calm":       {`{"summary": "Trims PORT.", "escalate": false}`, "flash"},
		"escalating": {`{"summary": "x", "escalate": true, "reason": "Changes a contract."}`, "argon"},
	} {
		t.Run(name, func(t *testing.T) {
			dir, env := routed(t, testRouting)
			env["TRIAGE_REPLY"] = tc.reply
			env["TRIAGE_OUTPUT"] = cliOutputFile(t, map[string][2]int{"lite": {1, 0}})

			if _, err := runTool(t, &fakeGitHub{}, dir, env, "decide"); err != nil {
				t.Fatalf("decide: %v", err)
			}

			if got := readText(t, env["GITHUB_OUTPUT"]); !strings.HasSuffix(got, "model="+tc.model+"\ntier="+map[string]string{"flash": tierStandard, "argon": tierHighRisk}[tc.model]+"\n") {
				t.Errorf("outputs = %q, want model %s", got, tc.model)
			}
			if summary := readText(t, env["GITHUB_STEP_SUMMARY"]); !strings.Contains(summary, "Reviewed by `"+tc.model+"`") || !strings.Contains(summary, "- Triage by `lite`") {
				t.Errorf("summary = %q", summary)
			}
			if saved, _ := readRouting(dir); saved.Model != tc.model || saved.Triage == nil {
				t.Errorf("saved routing = %+v, want %s with the verdict", saved, tc.model)
			}
		})
	}
}

func TestDecideSkipsTriageWhenARuleFired(t *testing.T) {
	// The sample diff has 6 changed lines; a threshold of 5 makes it high-risk.
	dir, env := routed(t, strings.Replace(testRouting, `: 10,`, `: 5,`, 1))
	if got := readText(t, env["GITHUB_OUTPUT"]); !strings.HasPrefix(got, "needs_triage=false\n") {
		t.Fatalf("outputs = %q, want no triage", got)
	}

	if _, err := runTool(t, &fakeGitHub{}, dir, env, "decide"); err != nil {
		t.Fatalf("decide without a triage reply: %v", err)
	}

	if got := readText(t, env["GITHUB_OUTPUT"]); !strings.HasSuffix(got, "model=argon\ntier=high-risk\n") {
		t.Errorf("outputs = %q", got)
	}
	if summary := readText(t, env["GITHUB_STEP_SUMMARY"]); !strings.Contains(summary, "Triage: skipped") {
		t.Errorf("summary = %q", summary)
	}
}

func TestDecideFailsWithoutAUsableTriage(t *testing.T) {
	for name, tc := range map[string]struct {
		reply  string
		output map[string][2]int
	}{
		"no reply":                   {"", map[string][2]int{"lite": {1, 0}}},
		"another model answered":     {`{"summary": "x"}`, map[string][2]int{"other": {1, 0}}},
		"triage model never answers": {`{"summary": "x"}`, map[string][2]int{"lite": {3, 3}}},
	} {
		t.Run(name, func(t *testing.T) {
			dir, env := routed(t, testRouting)
			env["TRIAGE_REPLY"] = tc.reply
			env["TRIAGE_OUTPUT"] = cliOutputFile(t, tc.output)
			if _, err := runTool(t, &fakeGitHub{}, dir, env, "decide"); err == nil {
				t.Error("decide succeeded")
			}
		})
	}
}

func TestRouteAndDecideFailWithoutTheirInputs(t *testing.T) {
	for name, tc := range map[string]struct {
		command string
		env     map[string]string
	}{
		"route without a config":   {"route", nil},
		"route with a bad config":  {"route", map[string]string{"ROUTING_CONFIG": "/does/not/exist.json"}},
		"route before prepare":     {"route", map[string]string{"ROUTING_CONFIG": "<testRouting>"}},
		"decide before route":      {"decide", nil},
		"decide on a broken route": {"decide", map[string]string{"<broken>": ""}},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			env := map[string]string{}
			for k, v := range tc.env {
				switch {
				case v == "<testRouting>":
					env[k] = writeFile(t, testRouting)
				case k == "<broken>":
					if err := os.WriteFile(filepath.Join(dir, routingFile), []byte("{"), 0o644); err != nil {
						t.Fatal(err)
					}
				default:
					env[k] = v
				}
			}
			if _, err := runTool(t, &fakeGitHub{}, dir, env, tc.command); err == nil {
				t.Errorf("%s succeeded", tc.command)
			}
		})
	}
}

func TestConfirmModelFailsOnUnreadableOutput(t *testing.T) {
	if err := confirmModel(filepath.Join(t.TempDir(), "missing.log"), "m"); err == nil {
		t.Error("confirmModel accepted a missing file")
	}
	if err := confirmModel(writeFile(t, "Error: quota exceeded"), "m"); err == nil {
		t.Error("confirmModel accepted output that is not JSON")
	}
}

func TestAppendFileReportsUnwritableFiles(t *testing.T) {
	if err := appendFile(filepath.Join(t.TempDir(), "missing-dir", "output"), "x=1\n"); err == nil {
		t.Error("appendFile wrote into a directory that does not exist")
	}
	if err := appendFile("", "x=1\n"); err != nil {
		t.Errorf("appendFile with no file: %v", err)
	}
}
