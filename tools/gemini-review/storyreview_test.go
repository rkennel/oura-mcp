package main

import (
	"strings"
	"testing"
)

// twoCriteria is a story with criteria AC1 and AC2.
var twoCriteria = story{
	Key: "KAN-21", Summary: "Review against the story",
	Criteria: numberCriteria([]string{"Lists each criterion.", "Says when the story is missing."}),
}

func verdicts(entries ...criterionVerdict) *storyReply {
	return &storyReply{Criteria: entries}
}

// quoting is the quote of twoCriteria's criterion with the ID.
func quoting(id string) string {
	for _, c := range twoCriteria.Criteria {
		if strings.EqualFold(c.ID, id) {
			return c.quote()
		}
	}
	return ""
}

func met(id string) criterionVerdict {
	return criterionVerdict{ID: id, Quote: quoting(id), Verdict: criterionSatisfied, Reason: "Done.", Evidence: "review.go line 3"}
}

func TestAssessStoryKeepsAVerdictOnEachCriterion(t *testing.T) {
	reply := verdicts(met("ac2"), criterionVerdict{ID: "AC1", Quote: quoting("AC1"), Verdict: " Not_Satisfied ", Reason: "Missing."})
	reply.Outside = []outsideChange{{Path: "internal/oura/client.go", Line: 21, Change: "Doubles the timeout."}}

	got, err := assessStory(twoCriteria, reply)
	if err != nil {
		t.Fatalf("assessStory: %v", err)
	}

	if got.verdictFor("AC1") != criterionNotSatisfied || got.verdictFor("AC2") != criterionSatisfied || got.verdictFor("AC3") != "" {
		t.Errorf("verdicts = %+v", got.Criteria)
	}
	if got.Criteria[0].Criterion.ID != "AC1" || !got.unmet() || len(got.Outside) != 1 {
		t.Errorf("review = %+v, want the story's order, unmet, one change outside", got)
	}
}

func TestAssessStoryRejectsAnIncompleteJudgement(t *testing.T) {
	for name, tc := range map[string]struct {
		reply *storyReply
		want  string
	}{
		"no story":          {nil, "judges none of the acceptance criteria of KAN-21"},
		"criterion omitted": {verdicts(met("AC1")), "omits the acceptance criteria AC2"},
		"unknown criterion": {verdicts(met("AC1"), met("AC2"), met("AC3")), `names "AC3", which is not a criterion of KAN-21`},
		"unknown verdict": {verdicts(met("AC1"), criterionVerdict{ID: "AC2", Quote: quoting("AC2"), Verdict: "mostly", Reason: "r"}),
			`verdict on AC2 is "mostly"`},
		"no reason":    {verdicts(met("AC1"), criterionVerdict{ID: "AC2", Quote: quoting("AC2"), Verdict: criterionPartly}), "verdict on AC2 gives no reason"},
		"judged twice": {verdicts(met("AC1"), met("AC2"), met("AC1")), "judges AC1 twice"},
		"quote of another criterion": {verdicts(met("AC1"), criterionVerdict{ID: "AC2", Quote: quoting("AC1"), Verdict: criterionSatisfied, Reason: "r"}),
			`verdict on AC2 quotes "Lists each criterion.", which is not that criterion's Then line`},
		"no quote": {verdicts(met("AC1"), criterionVerdict{ID: "AC2", Verdict: criterionSatisfied, Reason: "r"}),
			`verdict on AC2 quotes ""`},
		"outside with no path": {&storyReply{Criteria: []criterionVerdict{met("AC1"), met("AC2")}, Outside: []outsideChange{{Change: "x"}}},
			"change outside the story 1 has no path"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := assessStory(twoCriteria, tc.reply); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestAssessStoryIgnoresAReplyWhenThereAreNoCriteria(t *testing.T) {
	got, err := assessStory(unavailableStory("", "no key"), verdicts(met("AC1")))
	if err != nil || len(got.Criteria) != 0 || got.unmet() {
		t.Errorf("assessStory = %+v, %v", got, err)
	}
}

func TestStorySectionShowsEachVerdictAndTheChangesOutside(t *testing.T) {
	review, err := assessStory(twoCriteria, &storyReply{
		Criteria: []criterionVerdict{met("AC1"), {ID: "AC2", Quote: quoting("AC2"), Verdict: criterionPartly, Reason: "Only | half.", Evidence: "main.go"}},
		Outside:  []outsideChange{{Path: "a.go", Line: 4, Change: "Renames a flag."}},
	})
	if err != nil {
		t.Fatal(err)
	}

	got := storySection(review)
	for _, part := range []string{
		"### Acceptance criteria of KAN-21\n\nReview against the story",
		"| **AC1** Lists each criterion. | ✅ satisfied | Done. | review.go line 3 |",
		`| **AC2** Says when the story is missing. | 🟡 partly satisfied | Only \| half. | main.go |`,
		"#### Changes outside the story\n\n- `a.go` line 4: Renames a flag.",
	} {
		if !strings.Contains(got, part) {
			t.Errorf("section lacks %q:\n%s", part, got)
		}
	}
	if criterionLabel(criterionNotSatisfied) != "❌ not satisfied" {
		t.Errorf("label = %q", criterionLabel(criterionNotSatisfied))
	}
}

func TestStorySectionSaysWhenNothingIsOutsideOrTheStoryIsMissing(t *testing.T) {
	review, err := assessStory(twoCriteria, verdicts(met("AC1"), met("AC2")))
	if err != nil {
		t.Fatal(err)
	}
	if got := storySection(review); !strings.Contains(got, "None: every change serves the story.") {
		t.Errorf("section = %s", got)
	}

	missing := storySection(storyReview{Story: unavailableStory("KAN-9", "story KAN-9 could not be read from Jira: jira answered status 502")})
	want := "### Acceptance criteria\n\nNot checked: story KAN-9 could not be read from Jira: jira answered status 502. The review above did not compare the change with a story.\n"
	if missing != want {
		t.Errorf("section = %q, want %q", missing, want)
	}
}

func TestACriterionIsQuotedByItsThenLine(t *testing.T) {
	for name, tc := range map[string]struct{ text, want string }{
		"scenario":           {"**Given** a key\n**When** it runs\n**Then** it lists `them`\n**And** stops", "it lists them"},
		"scenario on a line": {"Given x When f runs Then it returns err", "it returns err"},
		"no then":            {"Lists each criterion.\n\nIn order.", "In order."},
		"then ends the line": {"Ask and then", "Ask and then"},
	} {
		t.Run(name, func(t *testing.T) {
			if got := (criterion{Text: tc.text}).quote(); got != tc.want {
				t.Errorf("quote = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestAQuoteMatchesOnlyItsOwnCriterion(t *testing.T) {
	ac := criterion{Text: "**Given** a pull request that omits behaviour an acceptance criterion requires\n**When** the review runs\n**Then** that criterion is reported as not satisfied"}
	for quote, want := range map[string]bool{
		"that criterion is reported as not satisfied":           true,
		"Then that criterion is reported as **not satisfied**.": true,
		"“That criterion is reported as not satisfied”":         true,
		"that criterion is reported":                            false,
		"Given a pull request that omits behaviour an acceptance criterion requires When the review runs Then that criterion is reported as not satisfied": true,
		"the review lists it as outside the story": false,
		"": false,
	} {
		if got := quotes(quote, ac); got != want {
			t.Errorf("quotes(%q) = %v, want %v", quote, got, want)
		}
	}
	if !quotes("ok", criterion{Text: "Then ok"}) {
		t.Error("a short Then line quoted whole does not match")
	}
	if got := clip(strings.Repeat("é", 90), 80); len([]rune(got)) != 81 {
		t.Errorf("clip = %q", got)
	}
}
