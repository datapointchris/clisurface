package clisurface

import (
	"slices"
	"testing"
)

func sectionTitles(body string) []string {
	var titles []string
	for _, s := range Sections(body) {
		titles = append(titles, s.Title)
	}
	return titles
}

// Each body is cut from what the tool prints, colors included, and the last
// three are the lines that look like headings and are not.
func TestSectionsFindsEachToolsHeadingsAndNoRowOrProse(t *testing.T) {
	for _, tc := range []struct {
		shape, body string
		want        []string
	}{
		{"clap", "Add dependencies to the project\n\n\x1b[1m\x1b[32mUsage:\x1b[0m \x1b[1m\x1b[36muv add\x1b[0m [OPTIONS]\n\n\x1b[1m\x1b[32mArguments:\x1b[0m\n  [PACKAGES]...  The packages to add\n\n\x1b[1m\x1b[32mCache options:\x1b[0m\n  -n, --no-cache\n          Avoid reading from the cache\n", []string{"Arguments", "Cache options"}},
		{"gh", "Work with GitHub pull requests.\n\n\x1b[0;1;39mUSAGE\x1b[0m\n  gh pr <command> [flags]\n\n\x1b[0;1;39mGENERAL COMMANDS\x1b[0m\n  create:        Create a pull request\n", []string{"USAGE", "GENERAL COMMANDS"}},
		{"git", "See 'git help <command>' to read about a specific subcommand\n\nMain Porcelain Commands\n   add                     Add file contents to the index\n\nDeveloper-facing file formats, protocols and other interfaces\n   gitattributes           Defining attributes per path\n", []string{"Main Porcelain Commands", "Developer-facing file formats, protocols and other interfaces"}},
		{"rules", "Subcommands\n───────────\nkernel  Start a kernel\n\nOptions\n━━━━━━━\n--debug  Log more\n", []string{"Subcommands", "Options"}},
		{"docker usage", "Usage:  docker run [OPTIONS] IMAGE\n\nCreate and run a new container\n\nAliases:\n  docker container run, docker run\n", []string{"Aliases"}},
		{"terraform row", "All other commands:\n  console   Try expressions\n\nmetadata  Metadata related commands:\n  functions   Show signatures\n", []string{"All other commands"}},
		{"prose", "The grammar is one word: helpnav TOOL.\n  helpnav docker\n\nFor more, read this paragraph.\n  helpnav uv\n", nil},
	} {
		if got := sectionTitles(tc.body); !slices.Equal(got, tc.want) {
			t.Errorf("%s: sections = %q, want %q", tc.shape, got, tc.want)
		}
	}
}

func TestSectionsRunToTheNextHeading(t *testing.T) {
	sections := Sections("intro\n\nOptions:\n  -a\n  -b\n\nFlags:\n  -c\n")
	want := []Section{{Title: "Options", Line: 2, End: 6}, {Title: "Flags", Line: 6, End: 9}}
	if !slices.Equal(sections, want) {
		t.Errorf("sections = %+v, want %+v", sections, want)
	}
}
