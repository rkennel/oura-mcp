package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"regexp"
	"strings"
)

// Review tiers. A standard pull request gets the fast model; a high-risk one
// gets the strongest.
const (
	tierStandard = "standard"
	tierHighRisk = "high-risk"
)

// routingConfig is the routing file, .github/gemini-review.json by default.
// Everything that decides which model reviews a pull request lives there, so
// it can change without a code change.
type routingConfig struct {
	Models struct {
		Triage   string `json:"triage"`
		Standard string `json:"standard"`
		HighRisk string `json:"high_risk"`
	} `json:"models"`
	HighRiskLineThreshold int            `json:"high_risk_line_threshold"`
	CriticalPaths         []criticalPath `json:"critical_paths"`
	SharedPackages        []string       `json:"shared_packages"`
}

// criticalPath is a path pattern whose changes always get the high-risk
// model, and why.
type criticalPath struct {
	Pattern string `json:"pattern"`
	Why     string `json:"why"`
}

func loadRoutingConfig(file string) (routingConfig, error) {
	var cfg routingConfig
	raw, err := os.ReadFile(file)
	if err != nil {
		return cfg, fmt.Errorf("read the routing config: %w", err)
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields() // A misspelt key must not be ignored silently.
	if err := decoder.Decode(&cfg); err != nil {
		return cfg, fmt.Errorf("routing config %s: %w", file, err)
	}
	if err := cfg.validate(); err != nil {
		return cfg, fmt.Errorf("routing config %s: %w", file, err)
	}
	return cfg, nil
}

func (cfg routingConfig) validate() error {
	var problems []string
	for name, model := range map[string]string{
		"models.triage": cfg.Models.Triage, "models.standard": cfg.Models.Standard, "models.high_risk": cfg.Models.HighRisk,
	} {
		if strings.TrimSpace(model) == "" {
			problems = append(problems, name+" is empty")
		}
	}
	if cfg.HighRiskLineThreshold <= 0 {
		problems = append(problems, "high_risk_line_threshold must be a positive number of lines")
	}
	for i, critical := range cfg.CriticalPaths {
		if strings.TrimSpace(critical.Why) == "" {
			problems = append(problems, fmt.Sprintf("critical_paths[%d] has no why", i))
		}
		if err := checkPattern(critical.Pattern); err != nil {
			problems = append(problems, fmt.Sprintf("critical_paths[%d]: %v", i, err))
		}
	}
	for i, pattern := range cfg.SharedPackages {
		if err := checkPattern(pattern); err != nil {
			problems = append(problems, fmt.Sprintf("shared_packages[%d]: %v", i, err))
		}
	}
	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}
	return nil
}

// checkPattern rejects a pattern that matchPath could never apply.
func checkPattern(pattern string) error {
	if strings.TrimSpace(pattern) == "" {
		return errors.New("empty pattern")
	}
	for _, segment := range strings.Split(pattern, "/") {
		if _, err := path.Match(segment, ""); err != nil {
			return fmt.Errorf("pattern %q: %w", pattern, err)
		}
	}
	return nil
}

// matchPath matches a slash-separated path against a pattern in which "**"
// stands for any number of directories and every other segment follows
// path.Match: "*" matches within one segment.
func matchPath(pattern, name string) bool {
	return matchSegments(strings.Split(pattern, "/"), strings.Split(name, "/"))
}

func matchSegments(pattern, name []string) bool {
	if len(pattern) == 0 {
		return len(name) == 0
	}
	if pattern[0] == "**" {
		for skip := 0; skip <= len(name); skip++ {
			if matchSegments(pattern[1:], name[skip:]) {
				return true
			}
		}
		return false
	}
	if len(name) == 0 {
		return false
	}
	matched, _ := path.Match(pattern[0], name[0]) // Patterns are checked on load.
	return matched && matchSegments(pattern[1:], name[1:])
}

// routing is the routing decision for one pull request, with the evidence
// behind it. route writes it, decide completes it and post reports it.
type routing struct {
	ChangedLines int            `json:"changed_lines"`
	Threshold    int            `json:"threshold"`
	CriticalHits []criticalHit  `json:"critical_hits"`
	APIChanges   []string       `json:"api_changes"`
	Triage       *triageVerdict `json:"triage,omitempty"`
	Tier         string         `json:"tier"`
	Model        string         `json:"model"`
	TriageModel  string         `json:"triage_model"`
	// HighRiskModel is kept so triage can escalate without the config.
	HighRiskModel string `json:"high_risk_model"`
}

type criticalHit struct {
	Path    string `json:"path"`
	Pattern string `json:"pattern"`
	Why     string `json:"why"`
}

// triageVerdict is the triage model's reply.
type triageVerdict struct {
	Summary  string `json:"summary"`
	Escalate bool   `json:"escalate"`
	Reason   string `json:"reason"`
}

// assess applies the routing file's rules to a pull request's diff. Any rule
// that fires makes the pull request high-risk; triage can only add to that.
func assess(files []fileDiff, cfg routingConfig) routing {
	r := routing{
		ChangedLines:  changedLines(files),
		Threshold:     cfg.HighRiskLineThreshold,
		CriticalHits:  criticalHits(files, cfg.CriticalPaths),
		APIChanges:    apiChanges(files, cfg.SharedPackages),
		TriageModel:   cfg.Models.Triage,
		HighRiskModel: cfg.Models.HighRisk,
	}
	r.Tier, r.Model = tierStandard, cfg.Models.Standard
	if r.ruleFired() {
		r.Tier, r.Model = tierHighRisk, cfg.Models.HighRisk
	}
	return r
}

func (r routing) ruleFired() bool {
	return r.ChangedLines >= r.Threshold || len(r.CriticalHits) > 0 || len(r.APIChanges) > 0
}

// needsTriage reports whether the triage model can still change the
// decision. Once a rule has made a pull request high-risk, it cannot.
func (r routing) needsTriage() bool {
	return !r.ruleFired()
}

// withTriage records the triage model's verdict and escalates on it.
func (r routing) withTriage(verdict triageVerdict) routing {
	r.Triage = &verdict
	if verdict.Escalate {
		r.Tier, r.Model = tierHighRisk, r.HighRiskModel
	}
	return r
}

func changedLines(files []fileDiff) int {
	count := 0
	for _, file := range files {
		for _, h := range file.hunks {
			for _, line := range h.lines {
				if line.kind != ' ' {
					count++
				}
			}
		}
	}
	return count
}

// criticalHits lists each changed file that a critical path pattern matches,
// under its new or its old name, so moving a file out of a critical path
// counts too.
func criticalHits(files []fileDiff, critical []criticalPath) []criticalHit {
	var hits []criticalHit
	for _, file := range files {
		for _, rule := range critical {
			name := file.path
			if !matchPath(rule.Pattern, name) {
				name = file.oldPath
				if name == "" || !matchPath(rule.Pattern, name) {
					continue
				}
			}
			hits = append(hits, criticalHit{Path: name, Pattern: rule.Pattern, Why: rule.Why})
			break // One reason per file is enough.
		}
	}
	return hits
}

var (
	// An exported top-level declaration: func Name, type Name, var Name,
	// const Name.
	exportedTopLevel = regexp.MustCompile(`^(?:func|type|var|const)\s+([A-Z]\w*)`)
	// A method of an exported type: func (r *Type) Name or func (Type[T]) Name.
	exportedMethod = regexp.MustCompile(`^func\s+\(\s*(?:\w+\s+)?\*?([A-Z]\w*)(?:\[[^\]]*\])?\s*\)\s*([A-Z]\w*)`)
	// The opening line of an exported struct or interface.
	exportedTypeOpen = regexp.MustCompile(`^type\s+([A-Z]\w*)(?:\[[^\]]*\])?\s+(struct|interface)\s*\{`)
	// A field of a struct or a method of an interface, one tab in.
	exportedMember = regexp.MustCompile(`^\t([A-Z]\w*)\b`)
)

// apiChanges finds changes to the exported Go API of the shared packages:
// an exported declaration or member that is removed or rewritten (its old
// line leaves the diff), and a method added to an exported interface, which
// breaks every implementation. Adding new exported names is not a change to
// an existing interface, so it does not count. This reads the diff alone,
// so it is a heuristic; triage covers what it misses.
func apiChanges(files []fileDiff, shared []string) []string {
	var changes []string
	seen := make(map[string]bool)
	for _, file := range files {
		if !isSharedGoSource(file.path, shared) {
			continue
		}
		for _, h := range file.hunks {
			for _, name := range hunkAPIChanges(h) {
				change := fmt.Sprintf("%s: %s", file.path, name)
				if !seen[change] {
					seen[change] = true
					changes = append(changes, change)
				}
			}
		}
	}
	return changes
}

func isSharedGoSource(name string, shared []string) bool {
	if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
		return false
	}
	for _, pattern := range shared {
		if matchPath(pattern, name) {
			return true
		}
	}
	return false
}

// hunkAPIChanges walks one hunk, tracking the exported type it is inside,
// and names each exported identifier the hunk changes.
func hunkAPIChanges(h hunk) []string {
	var names []string
	var openType, openKind string
	if _, context, ok := strings.Cut(strings.TrimPrefix(h.header, "@@"), "@@"); ok {
		openType, openKind = typeOpenedBy(strings.TrimSpace(context))
	}
	for _, line := range h.lines {
		if name, kind := typeOpenedBy(line.text); name != "" {
			openType, openKind = name, kind
		} else if line.text == "}" || exportedTopLevel.MatchString(line.text) || strings.HasPrefix(line.text, "func ") {
			openType, openKind = "", ""
		}
		if name := changedAPIName(line, openType, openKind); name != "" {
			names = append(names, name)
		}
	}
	return names
}

func typeOpenedBy(text string) (name, kind string) {
	if match := exportedTypeOpen.FindStringSubmatch(text); match != nil {
		return match[1], match[2]
	}
	return "", ""
}

func changedAPIName(line diffLine, openType, openKind string) string {
	removed := line.kind == '-'
	if match := exportedMethod.FindStringSubmatch(line.text); match != nil && removed {
		return match[1] + "." + match[2]
	}
	if match := exportedTopLevel.FindStringSubmatch(line.text); match != nil && removed {
		return match[1]
	}
	if openType == "" || line.kind == ' ' {
		return ""
	}
	member := exportedMember.FindStringSubmatch(line.text)
	if member == nil {
		return ""
	}
	if removed || openKind == "interface" {
		return openType + "." + member[1]
	}
	return ""
}

// parseTriageVerdict reads the triage model's reply, which should be one
// JSON object, possibly wrapped in a code fence or a sentence.
func parseTriageVerdict(reply string) (triageVerdict, error) {
	var verdict triageVerdict
	if err := unmarshalOutermostObject(reply, &verdict); err != nil {
		return verdict, fmt.Errorf("triage: %w", err)
	}
	if strings.TrimSpace(verdict.Summary) == "" {
		return verdict, errors.New("triage: the reply has no summary")
	}
	if verdict.Escalate && strings.TrimSpace(verdict.Reason) == "" {
		return verdict, errors.New("triage: the reply escalates without a reason")
	}
	return verdict, nil
}

// unmarshalOutermostObject decodes the text between a reply's first "{" and
// its last "}".
func unmarshalOutermostObject(reply string, target any) error {
	start := strings.Index(reply, "{")
	end := strings.LastIndex(reply, "}")
	if start < 0 || end < start {
		return errors.New("the reply holds no JSON object")
	}
	if err := json.Unmarshal([]byte(reply[start:end+1]), target); err != nil {
		return fmt.Errorf("the reply is not the expected JSON: %w", err)
	}
	return nil
}

// explain says in one sentence which model reviews the pull request and why.
func (r routing) explain() string {
	if r.Tier == tierHighRisk {
		return fmt.Sprintf("Reviewed by `%s` (high-risk tier) because it %s.", r.Model, joinClauses(r.escalationReasons()))
	}
	clauses := []string{
		fmt.Sprintf("has %d changed lines (high-risk from %d)", r.ChangedLines, r.Threshold),
		"touches no critical path",
		"changes no exported API of a shared package",
	}
	if r.Triage != nil {
		clauses = append(clauses, fmt.Sprintf("triage by `%s` found no changed shared interface", r.TriageModel))
	}
	return fmt.Sprintf("Reviewed by `%s` (standard tier) because it %s.", r.Model, joinClauses(clauses))
}

func (r routing) escalationReasons() []string {
	var reasons []string
	if r.ChangedLines >= r.Threshold {
		reasons = append(reasons, fmt.Sprintf("has %d changed lines (high-risk from %d)", r.ChangedLines, r.Threshold))
	}
	if len(r.CriticalHits) > 0 {
		reasons = append(reasons, "touches critical paths: "+describeHits(r.CriticalHits))
	}
	if len(r.APIChanges) > 0 {
		reasons = append(reasons, "changes the exported API of a shared package: `"+strings.Join(r.APIChanges, "`, `")+"`")
	}
	if r.Triage != nil && r.Triage.Escalate {
		reasons = append(reasons, fmt.Sprintf("triage by `%s` flagged it: %s", r.TriageModel, strings.TrimSpace(r.Triage.Reason)))
	}
	return reasons
}

// describeHits names the critical paths a pull request touches, one entry
// per rule: the file when the rule matched one, else the pattern and a count,
// so a change to many files in one critical directory stays one line.
func describeHits(hits []criticalHit) string {
	var order []string
	byPattern := make(map[string][]criticalHit)
	for _, hit := range hits {
		if _, seen := byPattern[hit.Pattern]; !seen {
			order = append(order, hit.Pattern)
		}
		byPattern[hit.Pattern] = append(byPattern[hit.Pattern], hit)
	}
	entries := make([]string, 0, len(order))
	for _, pattern := range order {
		matched := byPattern[pattern]
		if len(matched) == 1 {
			entries = append(entries, fmt.Sprintf("`%s` (%s)", matched[0].Path, matched[0].Why))
			continue
		}
		entries = append(entries, fmt.Sprintf("%d files in `%s` (%s)", len(matched), pattern, matched[0].Why))
	}
	return strings.Join(entries, ", ")
}

func joinClauses(clauses []string) string {
	switch len(clauses) {
	case 0:
		return ""
	case 1:
		return clauses[0]
	default:
		return strings.Join(clauses[:len(clauses)-1], "; ") + "; and " + clauses[len(clauses)-1]
	}
}
