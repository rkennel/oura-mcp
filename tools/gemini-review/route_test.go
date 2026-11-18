package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// testRouting is a routing file for the tests: a low threshold, one critical
// directory and one critical name pattern.
const testRouting = `{
  "models": {"triage": "lite", "standard": "flash", "high_risk": "argon"},
  "high_risk_line_threshold": 10,
  "critical_paths": [
    {"pattern": "cmd/server/**", "why": "the entry point"},
    {"pattern": "**/*token*", "why": "token handling"}
  ],
  "shared_packages": ["internal/**"]
}`

func writeFile(t *testing.T, content string) string {
	t.Helper()
	name := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(name, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return name
}

func testConfig(t *testing.T) routingConfig {
	t.Helper()
	cfg, err := loadRoutingConfig(writeFile(t, testRouting))
	if err != nil {
		t.Fatalf("loadRoutingConfig: %v", err)
	}
	return cfg
}

// diffOf builds a one-file diff whose single hunk has the given header
// context and lines, each starting with '+', '-' or ' '.
func diffOf(path, context string, lines ...string) string {
	var oldCount, newCount int
	for _, line := range lines {
		if line[0] != '+' {
			oldCount++
		}
		if line[0] != '-' {
			newCount++
		}
	}
	header := "@@ -1," + strconv.Itoa(oldCount) + " +1," + strconv.Itoa(newCount) + " @@"
	if context != "" {
		header += " " + context
	}
	return "diff --git a/" + path + " b/" + path + "\n--- a/" + path + "\n+++ b/" + path + "\n" +
		header + "\n" + strings.Join(lines, "\n") + "\n"
}

func assessDiff(t *testing.T, raw string) routing {
	t.Helper()
	return assess(mustParse(t, raw), testConfig(t))
}

func TestMatchPath(t *testing.T) {
	for _, tc := range []struct {
		pattern, name string
		want          bool
	}{
		{".github/workflows/**", ".github/workflows/ci.yml", true},
		{".github/workflows/**", ".github/workflows", true},
		{".github/workflows/**", ".github/gemini-review.json", false},
		{"**/*token*", "internal/oura/token_store.go", true},
		{"**/*token*", "tokens.go", true},
		{"**/*token*", "internal/oura/client.go", false},
		{"internal/**/*.go", "internal/app/config.go", true},
		{"go.mod", "go.mod", true},
		{"go.mod", "tools/gemini-review/go.mod", false},
		{"cmd/*/main.go", "cmd/server/main.go", true},
		{"cmd/*/main.go", "cmd/server/sub/main.go", false},
	} {
		if got := matchPath(tc.pattern, tc.name); got != tc.want {
			t.Errorf("matchPath(%q, %q) = %t, want %t", tc.pattern, tc.name, got, tc.want)
		}
	}
}

func TestLoadRoutingConfigRejectsBadFiles(t *testing.T) {
	for name, content := range map[string]string{
		"not JSON":       `models: flash`,
		"misspelt key":   strings.Replace(testRouting, "high_risk_line_threshold", "high_risk_lines", 1),
		"missing model":  strings.Replace(testRouting, `"argon"`, `""`, 1),
		"zero threshold": strings.Replace(testRouting, `: 10,`, `: 0,`, 1),
		"no why":         strings.Replace(testRouting, `"the entry point"`, `""`, 1),
		"bad pattern":    strings.Replace(testRouting, `cmd/server/**`, `cmd/[server`, 1),
		"empty pattern":  strings.Replace(testRouting, `"internal/**"`, `""`, 1),
		"bad shared":     strings.Replace(testRouting, `"internal/**"`, `"internal/[x"`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := loadRoutingConfig(writeFile(t, content)); err == nil {
				t.Error("loadRoutingConfig accepted it")
			}
		})
	}
	if _, err := loadRoutingConfig(filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Error("loadRoutingConfig accepted a missing file")
	}
}

// The repository's own routing file must load, so a typo in it fails here
// rather than in a review run. What it routes where is for its owner to
// change freely, so it is not pinned down here.
func TestTheRepositoryRoutingFileLoads(t *testing.T) {
	if _, err := loadRoutingConfig("../../.github/gemini-review.json"); err != nil {
		t.Fatalf("loadRoutingConfig: %v", err)
	}
}

func TestAssessRoutesASmallOrdinaryChangeToTheStandardModel(t *testing.T) {
	got := assessDiff(t, diffOf("internal/app/app.go", "func run() {", " a", "-b", "+c"))

	if got.Tier != tierStandard || got.Model != "flash" || got.ChangedLines != 2 {
		t.Errorf("routing = %+v, want standard on flash with 2 changed lines", got)
	}
	if !got.needsTriage() || got.TriageModel != "lite" {
		t.Errorf("routing = %+v, want triage by lite", got)
	}
}

func TestAssessEscalatesAtTheLineThreshold(t *testing.T) {
	lines := make([]string, 10)
	for i := range lines {
		lines[i] = "+line"
	}
	got := assessDiff(t, diffOf("docs/notes.md", "", lines...))

	if got.Tier != tierHighRisk || got.Model != "argon" || got.needsTriage() {
		t.Errorf("routing = %+v, want high-risk on argon without triage", got)
	}
	if !strings.Contains(got.explain(), "has 10 changed lines (high-risk from 10)") {
		t.Errorf("explain = %q", got.explain())
	}

	under := assessDiff(t, diffOf("docs/notes.md", "", lines[:9]...))
	if under.Tier != tierStandard {
		t.Errorf("9 lines routed %s, want standard", under.Tier)
	}
}

func TestAssessEscalatesOnCriticalPaths(t *testing.T) {
	for name, raw := range map[string]string{
		"directory":    diffOf("cmd/server/main.go", "", "+x"),
		"name pattern": diffOf("internal/oura/token_store.go", "", "+x"),
		"moved out":    "diff --git a/cmd/server/main.go b/old/main.go\nsimilarity index 100%\nrename from cmd/server/main.go\nrename to old/main.go\n",
	} {
		t.Run(name, func(t *testing.T) {
			got := assessDiff(t, raw)
			if got.Tier != tierHighRisk || len(got.CriticalHits) != 1 {
				t.Errorf("routing = %+v, want high-risk with one critical hit", got)
			}
			if !strings.Contains(got.explain(), "touches critical paths: `") {
				t.Errorf("explain = %q", got.explain())
			}
		})
	}
}

func TestExplainGroupsCriticalFilesByRule(t *testing.T) {
	raw := diffOf("cmd/server/main.go", "", "+x") + diffOf("cmd/server/flags.go", "", "+x") + diffOf("internal/oura/token.go", "", "+x")
	got := assessDiff(t, raw).explain()
	want := "touches critical paths: 2 files in `cmd/server/**` (the entry point), `internal/oura/token.go` (token handling)"
	if !strings.Contains(got, want) {
		t.Errorf("explain = %q, want it to contain %q", got, want)
	}
}

func TestAssessFindsChangesToTheExportedAPIOfSharedPackages(t *testing.T) {
	for name, tc := range map[string]struct {
		raw  string
		want []string
	}{
		"function signature changed": {
			diffOf("internal/app/config.go", "", "-func Load(getenv func(string) string) (Config, error) {", "+func Load(ctx context.Context) (Config, error) {"),
			[]string{"internal/app/config.go: Load"},
		},
		"method removed": {
			diffOf("internal/app/app.go", "", "-func (s *Server) Close() error {", "-\treturn nil", "-}"),
			[]string{"internal/app/app.go: Server.Close"},
		},
		"type removed": {
			diffOf("internal/app/app.go", "", "-type Options struct {", "-}"),
			[]string{"internal/app/app.go: Options"},
		},
		"struct field removed": {
			diffOf("internal/app/config.go", "type Config struct {", "-\tPort string", "+\tport string"),
			[]string{"internal/app/config.go: Config.Port"},
		},
		"interface method added": {
			diffOf("internal/oura/client.go", "", " type Sleep interface {", "+\tDaily(ctx context.Context) error", " }"),
			[]string{"internal/oura/client.go: Sleep.Daily"},
		},
		"new exported function": {
			diffOf("internal/app/app.go", "", "+func Version() string {", "+\treturn \"1\"", "+}"),
			nil,
		},
		"struct field added": {
			diffOf("internal/app/config.go", "type Config struct {", " \tPort string", "+\tHost string"),
			nil,
		},
		"unexported method of exported type": {
			diffOf("internal/app/app.go", "", "-func (s *Server) close() {"),
			nil,
		},
		"exported method of unexported type": {
			diffOf("internal/app/app.go", "", "-func (s *server) Close() {"),
			nil,
		},
		"member after the type closed": {
			diffOf("internal/app/app.go", "type Config struct {", " }", " func run() {", "-\tStart()"),
			nil,
		},
		"test file": {
			diffOf("internal/app/app_test.go", "", "-func TestRun(t *testing.T) {"),
			nil,
		},
		"outside the shared packages": {
			diffOf("tools/x/main.go", "", "-func Run() {"),
			nil,
		},
	} {
		t.Run(name, func(t *testing.T) {
			got := assessDiff(t, tc.raw).APIChanges
			if strings.Join(got, "|") != strings.Join(tc.want, "|") {
				t.Errorf("API changes = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestAssessListsEachAPIChangeOnce(t *testing.T) {
	raw := diffOf("internal/app/config.go", "type Config struct {", "-\tPort string", "+\tPort int") +
		diffOf("internal/app/config.go", "type Config struct {", "-\tPort string")
	got := assessDiff(t, raw)
	if len(got.APIChanges) != 1 || got.Tier != tierHighRisk {
		t.Errorf("routing = %+v, want one API change and high risk", got)
	}
	if !strings.Contains(got.explain(), "changes the exported API of a shared package: `internal/app/config.go: Config.Port`") {
		t.Errorf("explain = %q", got.explain())
	}
}

func TestTriageCanEscalateButNotDowngrade(t *testing.T) {
	standard := assessDiff(t, diffOf("internal/app/app.go", "", "+x"))

	calm := standard.withTriage(triageVerdict{Summary: "Adds a line."})
	if calm.Tier != tierStandard || calm.Model != "flash" {
		t.Errorf("calm triage routed to %s (%s)", calm.Model, calm.Tier)
	}
	if !strings.Contains(calm.explain(), "triage by `lite` found no changed shared interface") {
		t.Errorf("explain = %q", calm.explain())
	}

	flagged := standard.withTriage(triageVerdict{Summary: "Changes a tool schema.", Escalate: true, Reason: "The sleep tool's output changes."})
	if flagged.Tier != tierHighRisk || flagged.Model != "argon" {
		t.Errorf("escalating triage routed to %s (%s)", flagged.Model, flagged.Tier)
	}
	if !strings.Contains(flagged.explain(), "triage by `lite` flagged it: The sleep tool's output changes.") {
		t.Errorf("explain = %q", flagged.explain())
	}
}

func TestExplainSaysWhyAStandardReviewIsStandard(t *testing.T) {
	got := assessDiff(t, diffOf("internal/app/app.go", "", "+x")).explain()
	want := "Reviewed by `flash` (standard tier) because it has 1 changed lines (high-risk from 10); touches no critical path; and changes no exported API of a shared package."
	if got != want {
		t.Errorf("explain =\n%q\nwant\n%q", got, want)
	}
}

func TestParseTriageVerdict(t *testing.T) {
	verdict, err := parseTriageVerdict("```json\n{\"summary\": \"Small fix.\", \"escalate\": false}\n```")
	if err != nil || verdict.Summary != "Small fix." || verdict.Escalate {
		t.Errorf("verdict = %+v, err = %v", verdict, err)
	}
	for name, reply := range map[string]string{
		"empty":                  "",
		"no summary":             `{"summary": " ", "escalate": false}`,
		"escalate without why":   `{"summary": "x", "escalate": true, "reason": ""}`,
		"wrong type":             `{"summary": "x", "escalate": "yes"}`,
		"prose instead of JSON:": "This looks fine to me.",
	} {
		if _, err := parseTriageVerdict(reply); err == nil {
			t.Errorf("%s: parseTriageVerdict(%q) succeeded", name, reply)
		}
	}
}
