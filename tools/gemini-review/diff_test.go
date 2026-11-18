package main

import (
	"strings"
	"testing"
)

const sampleDiff = `diff --git a/internal/app/config.go b/internal/app/config.go
index 1111111..2222222 100644
--- a/internal/app/config.go
+++ b/internal/app/config.go
@@ -10,4 +10,5 @@ func load() {
 	port := getenv("PORT")
-	if port == "" {
+	if strings.TrimSpace(port) == "" {
+		// Cloud Run always sets PORT; this is for local runs.
 		port = "8080"
 	}
diff --git a/docs/new.md b/docs/new.md
new file mode 100644
index 0000000..3333333
--- /dev/null
+++ b/docs/new.md
@@ -0,0 +1,2 @@
+++ a line that looks like a header
+second
\ No newline at end of file
diff --git a/logo.png b/logo.png
index 4444444..5555555 100644
Binary files a/logo.png and b/logo.png differ
`

func mustParse(t *testing.T, raw string) []fileDiff {
	t.Helper()
	files, err := parseDiff(raw)
	if err != nil {
		t.Fatalf("parseDiff: %v", err)
	}
	return files
}

func TestParseDiffNumbersEveryLine(t *testing.T) {
	files := mustParse(t, sampleDiff)

	if len(files) != 3 {
		t.Fatalf("got %d files, want 3", len(files))
	}
	want := []diffLine{
		{kind: ' ', text: "\tport := getenv(\"PORT\")", oldLine: 10, newLine: 10},
		{kind: '-', text: "\tif port == \"\" {", oldLine: 11},
		{kind: '+', text: "\tif strings.TrimSpace(port) == \"\" {", newLine: 11},
		{kind: '+', text: "\t\t// Cloud Run always sets PORT; this is for local runs.", newLine: 12},
		{kind: ' ', text: "\t\tport = \"8080\"", oldLine: 12, newLine: 13},
		{kind: ' ', text: "\t}", oldLine: 13, newLine: 14},
	}
	got := files[0].hunks[0].lines
	if len(got) != len(want) {
		t.Fatalf("got %d lines, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestParseDiffKeepsAddedLinesThatLookLikeHeaders(t *testing.T) {
	files := mustParse(t, sampleDiff)

	newFile := files[1]
	if newFile.path != "docs/new.md" {
		t.Errorf("path = %q, want docs/new.md", newFile.path)
	}
	lines := newFile.hunks[0].lines
	if len(lines) != 2 || lines[0].text != "++ a line that looks like a header" || lines[1].newLine != 2 {
		t.Errorf("lines = %+v, want the two added lines numbered 1 and 2", lines)
	}
}

func TestParseDiffNamesFilesWithoutTextChanges(t *testing.T) {
	files := mustParse(t, sampleDiff)

	if got := files[2]; got.path != "logo.png" || len(got.hunks) != 0 {
		t.Errorf("binary file = %+v, want logo.png with no hunks", got)
	}
}

func TestParseDiffIgnoresTextBeforeTheFirstFile(t *testing.T) {
	files := mustParse(t, "preamble\n"+sampleDiff)

	if len(files) != 3 {
		t.Errorf("got %d files, want 3", len(files))
	}
}

func TestParseDiffRejectsMalformedHunkHeaders(t *testing.T) {
	for _, header := range []string{"@@ -1 @@", "@@ 1,2 +1,2 @@", "@@ -1,2 1,2 @@", "@@ -x,2 +1,2 @@"} {
		raw := "diff --git a/f b/f\n--- a/f\n+++ b/f\n" + header + "\n"
		if _, err := parseDiff(raw); err == nil {
			t.Errorf("parseDiff accepted hunk header %q", header)
		}
	}
}

func TestAnchorsCoverBothSidesOfTheDiff(t *testing.T) {
	got := anchors(mustParse(t, sampleDiff))

	const path = "internal/app/config.go"
	for _, a := range []anchor{
		{path, sideRight, 10}, {path, sideRight, 11}, {path, sideRight, 14},
		{path, sideLeft, 10}, {path, sideLeft, 11}, {path, sideLeft, 13},
		{"docs/new.md", sideRight, 2},
	} {
		if !got[a] {
			t.Errorf("anchor %+v is missing", a)
		}
	}
	for _, a := range []anchor{
		{path, sideRight, 9}, {path, sideRight, 15}, {path, sideLeft, 14},
		{"docs/new.md", sideLeft, 1}, {"logo.png", sideRight, 1},
	} {
		if got[a] {
			t.Errorf("anchor %+v is off the diff but was listed", a)
		}
	}
}

func TestAnnotateShowsLineNumbersAndFiles(t *testing.T) {
	got := annotate(mustParse(t, sampleDiff))

	for _, want := range []string{
		"### File: internal/app/config.go",
		"@@ -10,4 +10,5 @@ func load() {",
		"L10    R10    | \tport := getenv(\"PORT\")",
		"L11           |-\tif port == \"\" {",
		"       R11    |+\tif strings.TrimSpace(port) == \"\" {",
		"### File: logo.png\n\n(no text changes",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("annotated diff lacks %q:\n%s", want, got)
		}
	}
}
