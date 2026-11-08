package clisurface

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Section is one heading of a help screen and the lines under it, up to the
// next heading.
type Section struct {
	// Title is the heading's words, without colors or a trailing colon.
	Title string `json:"title"`
	// Line is the heading's index among the screen's lines, from 0.
	Line int `json:"line"`
	// End is one past the section's last line.
	End int `json:"end"`
}

// maxSectionTitleWords is the most words a heading carries. A longer line is
// read as prose, so a heading longer than this is not found. git help -a
// prints a seven-word heading.
const maxSectionTitleWords = 8

// Sections splits a help screen, such as [Node.Body], at its headings. Lines
// are counted as strings.Split(body, "\n") counts them, and the lines above the
// first heading belong to no section.
//
// A heading is a short line at the left edge, after a blank line, with an
// indented line or a rule under it. That one shape covers clap's "Options:",
// gh's "GENERAL COMMANDS", cobra's "Global Flags:", git's "Main Porcelain
// Commands" and a man page's "DESCRIPTION". It must be the whole line, as
// [listsCommands] holds a command heading to be, so a row carrying its
// description after two spaces is never one. It carries nothing after a colon
// either, so "Usage: uv add [OPTIONS]" is a line of text, and the description
// docker prints under its usage line belongs to no section.
func Sections(body string) []Section {
	lines := strings.Split(body, "\n")
	plain := make([]string, len(lines))
	for i, line := range lines {
		plain[i] = strings.TrimRight(stripANSI(line), " \t\r")
	}
	var sections []Section
	for i := range plain {
		if !isSectionHeading(plain, i) {
			continue
		}
		if n := len(sections); n > 0 {
			sections[n-1].End = i
		}
		title := strings.TrimSpace(strings.TrimSuffix(plain[i], ":"))
		sections = append(sections, Section{Title: title, Line: i, End: len(lines)})
	}
	return sections
}

func isSectionHeading(lines []string, i int) bool {
	line := lines[i]
	if line == "" || line[0] == ' ' || line[0] == '\t' || i+1 >= len(lines) {
		return false
	}
	if i > 0 && strings.TrimSpace(lines[i-1]) != "" {
		return false
	}
	if strings.Contains(line, "  ") || strings.Contains(line, "\t") || len(strings.Fields(line)) > maxSectionTitleWords {
		return false
	}
	if colon := strings.IndexByte(line, ':'); colon >= 0 && colon != len(line)-1 {
		return false
	}
	last, _ := utf8.DecodeLastRuneInString(line)
	if !unicode.IsLetter(last) && !unicode.IsDigit(last) && last != ':' && last != ')' {
		return false
	}
	next := lines[i+1]
	if strings.TrimSpace(next) == "" {
		return false
	}
	return isRule(strings.TrimSpace(next)) || next[0] == ' ' || next[0] == '\t'
}
