package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// cleanTree is a repository that keeps every mechanical rule.
var cleanTree = map[string]string{
	".env.example": "# The key.\nCONNECTOR_API_KEY=change-me\n# OURA_API_BASE_URL=https://api.ouraring.com\n",
	"README.md":    "| Variable | Purpose |\n|---|---|\n| `CONNECTOR_API_KEY` | Key. |\n| `OURA_API_BASE_URL` | Oura. |\n",
	"internal/app/config.go": "package app\n\nconst apiKeyVariable = \"CONNECTOR_API_KEY\"\n\n" +
		"func load(getenv func(string) string) (string, string) {\n" +
		"\treturn getenv(apiKeyVariable), getenv(\"OURA_API_BASE_URL\")\n}\n",
	"internal/app/config_test.go": "package app\n\nimport \"os\"\n\nvar _ = os.Getenv(\"TESTS_MAY\")\n",
	"cmd/server/main.go":          "package main\n\nimport \"os\"\n\nfunc main() { run(os.Getenv) }\n\nfunc run(func(string) string) {}\n",
	"api-explore/mcp.http":        "@baseUrl = http://localhost:8080\n@clientSecret = e2e-client-secret\n\nPOST {{baseUrl}}/mcp?start_date=2026-11-01&end_date=2026-11-01\nx-api-key: {{apiKey}}\nAuthorization: Bearer {{issue.response.body.access_token}}\n",
	".git/ignored.http":           "x-api-key: real-key-in-a-hidden-directory",
}

// writeTree writes files into a new directory and returns it.
func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, content := range files {
		full := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// treeWith is cleanTree with some files replaced or added.
func treeWith(changes map[string]string) map[string]string {
	tree := map[string]string{}
	for name, content := range cleanTree {
		tree[name] = content
	}
	for name, content := range changes {
		tree[name] = content
	}
	return tree
}

// addedFile is a diff that adds a file with the given lines.
func addedFile(path string, lines ...string) string {
	var b strings.Builder
	b.WriteString("diff --git a/" + path + " b/" + path + "\nnew file mode 100644\n--- /dev/null\n+++ b/" + path + "\n")
	b.WriteString("@@ -0,0 +1," + strconv.Itoa(len(lines)) + " @@\n")
	for _, line := range lines {
		b.WriteString("+" + line + "\n")
	}
	return b.String()
}

// changedFile is a diff that replaces line 10 of a file.
func changedFile(path, removed, added string) string {
	return "diff --git a/" + path + " b/" + path + "\n--- a/" + path + "\n+++ b/" + path + "\n" +
		"@@ -10,2 +10,2 @@\n context\n-" + removed + "\n+" + added + "\n"
}

func check(t *testing.T, tree map[string]string, diff string) []finding {
	t.Helper()
	files, err := parseDiff(diff)
	if err != nil {
		t.Fatal(err)
	}
	findings, err := checkStandards(writeTree(t, tree), files)
	if err != nil {
		t.Fatalf("checkStandards: %v", err)
	}
	return findings
}

func TestCheckStandardsPassesACleanChange(t *testing.T) {
	diff := addedFile("internal/oura/days.go", "package oura") +
		changedFile("internal/httpapi/handler.go", `mux.Handle("/x", h)`, `mux.Handle("/y", h)`) +
		addedFile("api-explore/y.http", "GET {{baseUrl}}/y") +
		changedFile("scripts/start-server.sh", "# old comment", "# new comment")

	if findings := check(t, cleanTree, diff); len(findings) != 0 {
		t.Errorf("findings = %v, want none", findings)
	}
}

func TestCheckStandardsFlagsEachMechanicalRule(t *testing.T) {
	for name, tc := range map[string]struct {
		tree map[string]string
		diff string
		want finding
	}{
		"a new route with no .http change": {
			cleanTree,
			changedFile("internal/httpapi/handler.go", "\t// routes", "\tmux.HandleFunc(\"GET /version\", handleVersion)"),
			finding{Rule: "api-explore-file", Path: "internal/httpapi/handler.go", Line: 11, Side: sideRight},
		},
		"a removed route with no .http change": {
			cleanTree,
			changedFile("internal/httpapi/handler.go", "const MCPPath = \"/mcp\"", "// gone"),
			finding{Rule: "api-explore-file", Path: "internal/httpapi/handler.go", Line: 11, Side: sideLeft},
		},
		"a start script change with no test change": {
			cleanTree,
			changedFile("scripts/stop-server.sh", "kill \"$pid\"", "kill -9 \"$pid\""),
			finding{Rule: "scripts-tested", Path: "scripts/stop-server.sh", Line: 11, Side: sideRight},
		},
		"a literal key header in an .http file": {
			treeWith(map[string]string{"api-explore/bad.http": "GET /mcp\nx-api-key: my-real-key\n"}),
			"",
			finding{Rule: "http-no-secrets", Path: "api-explore/bad.http", Line: 2, Side: sideRight},
		},
		"a generated secret in an .http variable": {
			treeWith(map[string]string{"api-explore/bad.http": "@apiKey = 3f9a1c0b7e2d4f6a8b9c0d1e2f3a4b5c\n"}),
			"",
			finding{Rule: "http-no-secrets", Path: "api-explore/bad.http", Line: 1, Side: sideRight},
		},
		"a token in a query parameter": {
			treeWith(map[string]string{"api-explore/bad.http": "GET /x?access_token=Q2xhdWRlUmVhbFRva2VuMTIzNDU2\n"}),
			"",
			finding{Rule: "http-no-secrets", Path: "api-explore/bad.http", Line: 1, Side: sideRight},
		},
		"a GitHub token anywhere": {
			treeWith(map[string]string{"docs/notes.http": "# ghp_abcdefghijklmnopqrstuvwxyz0123456789\n"}),
			"",
			finding{Rule: "http-no-secrets", Path: "docs/notes.http", Line: 1, Side: sideRight},
		},
		"the environment read outside internal/app": {
			treeWith(map[string]string{"internal/oura/client.go": "package oura\n\nimport \"os\"\n\nfunc token() string {\n\treturn os.Getenv(\"OURA_TOKEN\")\n}\n"}),
			"",
			finding{Rule: "config-in-app", Path: "internal/oura/client.go", Line: 6, Side: sideRight},
		},
		"a setting missing from .env.example": {
			treeWith(map[string]string{
				"internal/app/log.go": "package app\n\nfunc level(getenv func(string) string) string {\n\treturn getenv(\"LOG_LEVEL\")\n}\n",
				"README.md":           cleanTree["README.md"] + "| `LOG_LEVEL` | Logging. |\n",
			}),
			"",
			finding{Rule: "settings-documented", Path: "internal/app/log.go", Line: 4, Side: sideRight},
		},
	} {
		t.Run(name, func(t *testing.T) {
			findings := check(t, tc.tree, tc.diff)
			if len(findings) != 1 {
				t.Fatalf("findings = %v, want one", findings)
			}
			got := findings[0]
			got.Message = ""
			if got != tc.want {
				t.Errorf("finding = %+v, want %+v", got, tc.want)
			}
			if findings[0].Message == "" || !strings.Contains(findings[0].String(), "["+tc.want.Rule+"]") {
				t.Errorf("finding %q does not name its rule and explain itself", findings[0])
			}
		})
	}
}

func TestCheckStandardsNamesEveryPlaceAMissingSettingIsMissingFrom(t *testing.T) {
	tree := treeWith(map[string]string{
		"internal/app/log.go": "package app\n\nfunc level(getenv func(string) string) string {\n\treturn getenv(\"LOG_LEVEL\")\n}\n",
	})

	findings := check(t, tree, "")

	if len(findings) != 1 || !strings.Contains(findings[0].Message, ".env.example and the README's configuration table") {
		t.Errorf("findings = %v, want LOG_LEVEL missing from both", findings)
	}
}

func TestCheckStandardsReportsUnreadableTrees(t *testing.T) {
	for name, tree := range map[string]map[string]string{
		"broken Go":         treeWith(map[string]string{"internal/app/bad.go": "package app\n\nfunc {"}),
		"no .env.example":   {"README.md": "x", "internal/app/a.go": "package app\n"},
		"no README":         {".env.example": "x", "internal/app/a.go": "package app\n"},
		"unreadable folder": treeWith(nil),
	} {
		t.Run(name, func(t *testing.T) {
			root := writeTree(t, tree)
			if name == "unreadable folder" {
				root = filepath.Join(root, "missing")
			}
			if _, err := checkStandards(root, nil); err == nil {
				t.Error("checkStandards succeeded")
			}
		})
	}
}

func TestLooksLikeSecretSparesPlaceholders(t *testing.T) {
	for value, want := range map[string]bool{
		"change-me-local-dev-key":            false,
		"e2e-client-secret":                  false,
		"{{apiKey}}":                         false,
		"3f9a1c0b7e2d4f6a8b9c0d1e2f3a4b5c":   true,
		"T3VyYUFjY2Vzc1Rva2VuMTIzNDU2Nzg5MA": true,
	} {
		if got := looksLikeSecret(value); got != want {
			t.Errorf("looksLikeSecret(%q) = %t, want %t", value, got, want)
		}
	}
}

func TestStandardsCommandWritesItsResultAndFailsOnABreach(t *testing.T) {
	root := writeTree(t, treeWith(map[string]string{"api-explore/bad.http": "x-api-key: my-real-key\n"}))
	diffFile := filepath.Join(t.TempDir(), "pr.diff")
	if err := os.WriteFile(diffFile, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	resultFile := filepath.Join(t.TempDir(), "standards.json")
	var out strings.Builder

	err := standardsCommand(root, diffFile, resultFile, &out)

	if err == nil || !strings.Contains(err.Error(), "1 breaches") {
		t.Errorf("err = %v, want one breach", err)
	}
	if !strings.Contains(out.String(), "FAIL [http-no-secrets] api-explore/bad.http:1:") {
		t.Errorf("output = %q, want the breach and its rule", out.String())
	}
	result, err := readStandardsResult(filepath.Dir(resultFile))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Findings) != 1 || len(result.Rules) != len(checkedRules) {
		t.Errorf("result = %+v", result)
	}
}

func TestStandardsCommandPassesACleanRepository(t *testing.T) {
	diffFile := filepath.Join(t.TempDir(), "pr.diff")
	if err := os.WriteFile(diffFile, []byte(sampleDiff), 0o644); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder

	if err := standardsCommand(writeTree(t, cleanTree), diffFile, "", &out); err != nil {
		t.Fatalf("standards: %v", err)
	}
	if !strings.Contains(out.String(), "hold") {
		t.Errorf("output = %q", out.String())
	}
}

func TestStandardsCommandNeedsADiff(t *testing.T) {
	var out strings.Builder
	if err := standardsCommand(".", "", "", &out); err == nil {
		t.Error("standards succeeded without DIFF_FILE")
	}
	if err := standardsCommand(".", filepath.Join(t.TempDir(), "none"), "", &out); err == nil {
		t.Error("standards succeeded with a missing diff")
	}
}
