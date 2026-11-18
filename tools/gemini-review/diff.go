package main

import (
	"fmt"
	"strconv"
	"strings"
)

// Sides of a diff, as the GitHub API names them: LEFT is the base version
// of a file and RIGHT is the pull request's version.
const (
	sideLeft  = "LEFT"
	sideRight = "RIGHT"
)

// fileDiff is the part of a unified diff that changes one file.
type fileDiff struct {
	path    string
	oldPath string // the base version's path; empty for a new file
	hunks   []hunk
}

type hunk struct {
	header string
	lines  []diffLine
}

// diffLine is one line of a hunk. oldLine and newLine are the line's numbers
// in the base and the pull request's version; zero means the line is absent
// from that version.
type diffLine struct {
	kind    byte // '+', '-' or ' '
	text    string
	oldLine int
	newLine int
}

// anchor is a place on a pull request's diff where GitHub accepts a review
// comment.
type anchor struct {
	path string
	side string
	line int
}

// parseDiff reads a git unified diff, as GitHub returns for a pull request.
func parseDiff(raw string) ([]fileDiff, error) {
	var files []fileDiff
	var oldLine, newLine int

	for _, line := range strings.Split(raw, "\n") {
		if strings.HasPrefix(line, "diff --git ") {
			files = append(files, fileDiff{path: pathFromGitHeader(line)})
			continue
		}
		if len(files) == 0 {
			continue // Anything before the first file is not part of a file.
		}
		current := &files[len(files)-1]

		if strings.HasPrefix(line, "@@ ") {
			var err error
			oldLine, newLine, err = parseHunkHeader(line)
			if err != nil {
				return nil, fmt.Errorf("file %s: %w", current.path, err)
			}
			current.hunks = append(current.hunks, hunk{header: line})
			continue
		}
		if len(current.hunks) == 0 {
			// The file's header: "index", "--- a/x", "+++ b/x" and the like.
			// Only here can a "+++ " line be a header rather than an added
			// line that starts with "++".
			if path, ok := strings.CutPrefix(line, "+++ b/"); ok {
				current.path = path
			}
			if path, ok := strings.CutPrefix(line, "--- a/"); ok {
				current.oldPath = path
			}
			if path, ok := strings.CutPrefix(line, "rename from "); ok {
				current.oldPath = path
			}
			continue
		}
		if line == "" || !strings.ContainsRune("+- ", rune(line[0])) {
			// "\ No newline at end of file", or the empty string after the
			// diff's final newline.
			continue
		}

		parsed := diffLine{kind: line[0], text: line[1:]}
		if parsed.kind != '+' {
			parsed.oldLine = oldLine
			oldLine++
		}
		if parsed.kind != '-' {
			parsed.newLine = newLine
			newLine++
		}
		h := &current.hunks[len(current.hunks)-1]
		h.lines = append(h.lines, parsed)
	}
	return files, nil
}

// pathFromGitHeader takes the new path from a "diff --git a/x b/y" line. It
// stands until a "+++ b/y" line confirms it, and is the only path given for
// a file that is renamed without changes or is binary.
func pathFromGitHeader(line string) string {
	if i := strings.LastIndex(line, " b/"); i >= 0 {
		return line[i+len(" b/"):]
	}
	return strings.TrimPrefix(line, "diff --git ")
}

// parseHunkHeader returns the first old and new line numbers of a hunk from
// a header such as "@@ -10,6 +12,8 @@ func name".
func parseHunkHeader(header string) (oldStart, newStart int, err error) {
	fields := strings.Fields(header)
	if len(fields) < 3 {
		return 0, 0, fmt.Errorf("malformed hunk header %q", header)
	}
	oldStart, err = rangeStart(fields[1], "-")
	if err != nil {
		return 0, 0, fmt.Errorf("hunk header %q: %w", header, err)
	}
	newStart, err = rangeStart(fields[2], "+")
	if err != nil {
		return 0, 0, fmt.Errorf("hunk header %q: %w", header, err)
	}
	return oldStart, newStart, nil
}

// rangeStart reads the start of a range such as "-10,6" or "+12".
func rangeStart(field, prefix string) (int, error) {
	value, ok := strings.CutPrefix(field, prefix)
	if !ok {
		return 0, fmt.Errorf("range %q does not start with %q", field, prefix)
	}
	start, _, _ := strings.Cut(value, ",")
	n, err := strconv.Atoi(start)
	if err != nil {
		return 0, fmt.Errorf("range %q: %w", field, err)
	}
	return n, nil
}

// anchors lists every line of the diff that can carry a review comment:
// added and unchanged lines on the RIGHT, removed and unchanged lines on the
// LEFT.
func anchors(files []fileDiff) map[anchor]bool {
	result := make(map[anchor]bool)
	for _, file := range files {
		for _, h := range file.hunks {
			for _, line := range h.lines {
				if line.newLine > 0 {
					result[anchor{path: file.path, side: sideRight, line: line.newLine}] = true
				}
				if line.oldLine > 0 {
					result[anchor{path: file.path, side: sideLeft, line: line.oldLine}] = true
				}
			}
		}
	}
	return result
}

// annotate renders the diff with each line's numbers in front of it, so the
// reviewer can name the exact line a comment belongs to.
func annotate(files []fileDiff) string {
	var b strings.Builder
	for _, file := range files {
		fmt.Fprintf(&b, "### File: %s\n\n", file.path)
		if len(file.hunks) == 0 {
			b.WriteString("(no text changes: binary, or renamed without edits)\n\n")
			continue
		}
		for _, h := range file.hunks {
			b.WriteString(h.header + "\n")
			for _, line := range h.lines {
				fmt.Fprintf(&b, "%-6s %-6s |%c%s\n",
					lineLabel("L", line.oldLine), lineLabel("R", line.newLine), line.kind, line.text)
			}
			b.WriteString("\n")
		}
	}
	return b.String()
}

func lineLabel(prefix string, n int) string {
	if n == 0 {
		return ""
	}
	return prefix + strconv.Itoa(n)
}
