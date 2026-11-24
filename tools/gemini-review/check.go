package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// finding is one breach of a mechanical rule of GEMINI.md, at a line of a
// file. Side is LEFT when the line is a removed one, as in the diff.
type finding struct {
	Rule    string `json:"rule"`
	Path    string `json:"path"`
	Line    int    `json:"line"`
	Side    string `json:"side"`
	Message string `json:"message"`
}

func (f finding) String() string {
	return fmt.Sprintf("[%s] %s:%d: %s", f.Rule, f.Path, f.Line, f.Message)
}

// checkedRules are the GEMINI.md rules the mechanical check covers. GEMINI.md
// marks the same rules as checked by scripts/check-standards.sh, and a test
// keeps the two in step.
var checkedRules = []string{"api-explore-file", "http-no-secrets", "scripts-tested", "config-in-app", "settings-documented"}

// checkStandards checks the repository at root, and the change in files, for
// breaches of the mechanical rules. The diff-based rules look only at the
// change; the others look at the whole tree, so a breach cannot hide in an
// unchanged file.
func checkStandards(root string, files []fileDiff) ([]finding, error) {
	findings := append(checkRoutesExplored(files), checkServerScriptsTested(files)...)
	treeChecks := []func(string) ([]finding, error){checkHTTPFiles, checkEnvironmentReads, checkSettingsDocumented}
	for _, check := range treeChecks {
		found, err := check(root)
		if err != nil {
			return nil, err
		}
		findings = append(findings, found...)
	}
	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].Path != findings[j].Path {
			return findings[i].Path < findings[j].Path
		}
		return findings[i].Line < findings[j].Line
	})
	return findings, nil
}

// routeLine matches a line that registers or names an HTTP route:
// mux.Handle("GET /x", ...), mux.HandleFunc(...), or a constant such as
// MCPPath = "/mcp".
var routeLine = regexp.MustCompile(`\.Handle(Func)?\(|Path\s*=\s*"/`)

// checkRoutesExplored flags each added or removed route line in the
// server's Go code when nothing under api-explore/ changed.
func checkRoutesExplored(files []fileDiff) []finding {
	for _, file := range files {
		if strings.HasPrefix(file.path, "api-explore/") {
			return nil
		}
	}
	var findings []finding
	for _, file := range files {
		if !isServerSource(file.path) {
			continue
		}
		for _, line := range file.changes() {
			if routeLine.MatchString(line.text) {
				findings = append(findings, findingAt("api-explore-file", file, line,
					"adds or changes an HTTP route, but nothing under api-explore/ changed; add or update the route's .http file"))
			}
		}
	}
	return findings
}

// checkServerScriptsTested flags a changed line of behaviour in the start
// and stop scripts when their test script did not change. Comments and
// blank lines are not behaviour. An added line is named before a removed
// one, since it is the new behaviour.
func checkServerScriptsTested(files []fileDiff) []finding {
	const testScript = "scripts/server-scripts-test.sh"
	for _, file := range files {
		if file.path == testScript {
			return nil
		}
	}
	var findings []finding
	for _, file := range files {
		if file.path != "scripts/start-server.sh" && file.path != "scripts/stop-server.sh" {
			continue
		}
		if line, ok := firstBehaviourChange(file); ok {
			findings = append(findings, findingAt("scripts-tested", file, line,
				"changes the script's behaviour, but "+testScript+" did not change; add a case for it there"))
		}
	}
	return findings
}

func firstBehaviourChange(file fileDiff) (diffLine, bool) {
	var removed []diffLine
	for _, line := range file.changes() {
		text := strings.TrimSpace(line.text)
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		if line.kind == '+' {
			return line, true
		}
		removed = append(removed, line)
	}
	if len(removed) > 0 {
		return removed[0], true
	}
	return diffLine{}, false
}

func findingAt(ruleID string, file fileDiff, line diffLine, message string) finding {
	if line.kind == '-' {
		return finding{Rule: ruleID, Path: file.path, Line: line.oldLine, Side: sideLeft, Message: message}
	}
	return finding{Rule: ruleID, Path: file.path, Line: line.newLine, Side: sideRight, Message: message}
}

// isServerSource reports whether name is non-test Go code of the server.
func isServerSource(name string) bool {
	return (strings.HasPrefix(name, "cmd/") || strings.HasPrefix(name, "internal/")) &&
		strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go")
}

// Patterns of a credential in an .http file. A header or variable that
// carries a key must use a {{variable}}; any other value that looks like a
// generated secret is flagged wherever it appears.
var (
	credentialHeader  = regexp.MustCompile(`(?i)^\s*(x-api-key|authorization|cookie)\s*:\s*(.*)$`)
	credentialVar     = regexp.MustCompile(`(?i)^\s*@([a-z0-9_]*(key|token|secret|password)[a-z0-9_]*)\s*=\s*(\S+)`)
	credentialParam   = regexp.MustCompile(`(?i)[?&]?\b[a-z0-9_]*(key|token|secret|password)[a-z0-9_]*=([^&\s]+)`)
	knownTokenPattern = regexp.MustCompile(`\b(ghp_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,}|AIza[0-9A-Za-z_-]{30,}|sk-[A-Za-z0-9_-]{20,}|eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,})`)
	secretShape       = regexp.MustCompile(`^[A-Za-z0-9_\-+/=.~]{20,}$`)
	digit             = regexp.MustCompile(`[0-9]`)
	letter            = regexp.MustCompile(`[A-Za-z]`)
)

// checkHTTPFiles flags .http files that hold what looks like a real key or
// token.
func checkHTTPFiles(root string) ([]finding, error) {
	var findings []finding
	err := walkFiles(root, func(path string) error {
		if !strings.HasSuffix(path, ".http") {
			return nil
		}
		raw, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		for i, line := range strings.Split(string(raw), "\n") {
			if why := credentialIn(line); why != "" {
				findings = append(findings, finding{Rule: "http-no-secrets", Path: path, Line: i + 1, Side: sideRight, Message: why})
			}
		}
		return nil
	})
	return findings, err
}

// credentialIn says why line holds what looks like a real credential, or
// returns "" when it does not. Comments are checked too: a key pasted into
// one is still committed.
func credentialIn(line string) string {
	if match := credentialHeader.FindStringSubmatch(line); match != nil {
		if value := strings.TrimSpace(match[2]); value != "" && !strings.Contains(value, "{{") {
			return fmt.Sprintf("the %s header has a literal value; send it as a {{variable}} such as x-api-key: {{apiKey}}", match[1])
		}
	}
	if match := credentialVar.FindStringSubmatch(line); match != nil && looksLikeSecret(match[3]) {
		return fmt.Sprintf("@%s holds what looks like a real credential; take it from a REST Client environment variable instead", match[1])
	}
	for _, match := range credentialParam.FindAllStringSubmatch(line, -1) {
		if looksLikeSecret(match[2]) {
			return "a key, token, secret or password parameter holds what looks like a real credential; use a {{variable}}"
		}
	}
	if knownTokenPattern.MatchString(line) {
		return "holds what looks like a real API key or token"
	}
	return ""
}

// looksLikeSecret reports whether value looks generated rather than made up
// for an example: at least 20 characters of a token's alphabet, with both
// letters and digits. Placeholders such as change-me-local-dev-key and
// e2e-client-secret do not.
func looksLikeSecret(value string) bool {
	return !strings.Contains(value, "{{") && secretShape.MatchString(value) &&
		digit.MatchString(value) && letter.MatchString(value)
}

// environmentReaders are the calls that read the process environment.
var environmentReaders = map[string]bool{
	"os.Getenv": true, "os.LookupEnv": true, "os.Environ": true, "os.ExpandEnv": true,
	"syscall.Getenv": true, "syscall.Environ": true,
}

// checkEnvironmentReads flags calls that read the environment in the
// server's code outside internal/app. Passing os.Getenv as a value, as
// cmd/server does to hand it to internal/app, is wiring and allowed.
func checkEnvironmentReads(root string) ([]finding, error) {
	var findings []finding
	err := walkGoSources(root, func(path string, fset *token.FileSet, file *ast.File) {
		if strings.HasPrefix(path, "internal/app/") {
			return
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			if name := selectorName(call.Fun); environmentReaders[name] {
				findings = append(findings, finding{
					Rule: "config-in-app", Path: path, Line: fset.Position(call.Pos()).Line, Side: sideRight,
					Message: name + " reads the environment outside internal/app; read the setting in internal/app's configuration and pass it in",
				})
			}
			return true
		})
	})
	return findings, err
}

func selectorName(expr ast.Expr) string {
	selector, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	pkg, ok := selector.X.(*ast.Ident)
	if !ok {
		return ""
	}
	return pkg.Name + "." + selector.Sel.Name
}

// setting is an environment variable the server reads, and where.
type setting struct {
	name string
	path string
	line int
}

// checkSettingsDocumented flags each setting internal/app reads that is
// missing from .env.example or from the README's configuration table.
func checkSettingsDocumented(root string) ([]finding, error) {
	settings, err := settingsRead(root)
	if err != nil {
		return nil, err
	}
	envExample, err := os.ReadFile(filepath.Join(root, ".env.example"))
	if err != nil {
		return nil, fmt.Errorf("read .env.example: %w", err)
	}
	readme, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		return nil, fmt.Errorf("read README.md: %w", err)
	}

	var findings []finding
	for _, s := range settings {
		var missing []string
		if !regexp.MustCompile(`(?m)^#?\s*` + regexp.QuoteMeta(s.name) + `=`).Match(envExample) {
			missing = append(missing, ".env.example")
		}
		if !strings.Contains(string(readme), "| `"+s.name+"` |") {
			missing = append(missing, "the README's configuration table")
		}
		if len(missing) > 0 {
			findings = append(findings, finding{
				Rule: "settings-documented", Path: s.path, Line: s.line, Side: sideRight,
				Message: fmt.Sprintf("the server reads %s, which is missing from %s", s.name, strings.Join(missing, " and ")),
			})
		}
	}
	return findings, nil
}

// settingsRead finds the settings internal/app reads: each getenv call whose
// argument is a string literal or a string constant of the package. That is
// how internal/app reads its environment, through the getenv it is given.
func settingsRead(root string) ([]setting, error) {
	constants := map[string]string{}
	type call struct {
		path string
		line int
		arg  ast.Expr
	}
	var calls []call
	err := walkGoSources(root, func(path string, fset *token.FileSet, file *ast.File) {
		if !strings.HasPrefix(path, "internal/app/") {
			return
		}
		ast.Inspect(file, func(node ast.Node) bool {
			switch n := node.(type) {
			case *ast.ValueSpec:
				recordStringConstants(n, constants)
			case *ast.CallExpr:
				if fun, ok := n.Fun.(*ast.Ident); ok && fun.Name == "getenv" && len(n.Args) == 1 {
					calls = append(calls, call{path: path, line: fset.Position(n.Pos()).Line, arg: n.Args[0]})
				}
			}
			return true
		})
	})
	if err != nil {
		return nil, err
	}

	var settings []setting
	seen := map[string]bool{}
	for _, c := range calls {
		name := stringValue(c.arg, constants)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		settings = append(settings, setting{name: name, path: c.path, line: c.line})
	}
	return settings, nil
}

func recordStringConstants(spec *ast.ValueSpec, constants map[string]string) {
	for i, name := range spec.Names {
		if i < len(spec.Values) {
			if value := stringValue(spec.Values[i], nil); value != "" {
				constants[name.Name] = value
			}
		}
	}
}

func stringValue(expr ast.Expr, constants map[string]string) string {
	switch e := expr.(type) {
	case *ast.BasicLit:
		if e.Kind == token.STRING {
			if value, err := strconv.Unquote(e.Value); err == nil {
				return value
			}
		}
	case *ast.Ident:
		return constants[e.Name]
	}
	return ""
}

// walkGoSources parses each non-test Go file of the server, under cmd/ and
// internal/, and passes it to visit with its slash-separated path.
func walkGoSources(root string, visit func(path string, fset *token.FileSet, file *ast.File)) error {
	fset := token.NewFileSet()
	return walkFiles(root, func(path string) error {
		if !isServerSource(path) {
			return nil
		}
		file, err := parser.ParseFile(fset, filepath.Join(root, path), nil, parser.SkipObjectResolution)
		if err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		visit(path, fset, file)
		return nil
	})
}

// walkFiles calls visit with the slash-separated path, relative to root, of
// each file in the repository. Hidden directories such as .git and the
// git-ignored build output in bin/ are skipped.
func walkFiles(root string, visit func(path string) error) error {
	return filepath.WalkDir(root, func(full string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, full)
		if err != nil {
			return fmt.Errorf("path of %s: %w", full, err)
		}
		rel = filepath.ToSlash(rel)
		if entry.IsDir() {
			if rel != "." && (strings.HasPrefix(entry.Name(), ".") || rel == "bin") {
				return filepath.SkipDir
			}
			return nil
		}
		return visit(rel)
	})
}
