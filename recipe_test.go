package clisurface

import (
	"strings"
	"testing"
)

// gitHelpAll is `git help -a` cut to one section of each kind: commands, and
// the documentation topics that are not commands.
const gitHelpAll = `See 'git help <command>' to read about a specific subcommand

Main Porcelain Commands
   add                     Add file contents to the index
   worktree                Manage multiple working trees

Ancillary Commands / Manipulators
   remote                  Manage set of tracked repositories

Interacting with Others
   svn                     Bidirectional operation between a Subversion repository and Git

User-facing repository, command and file interfaces
   gitattributes           Defining attributes per path
   gitignore               Specifies intentionally untracked files to ignore
`

// The two "interfaces" sections hold documentation topics, not commands.
// `git gitignore -h` is not a command, and matching on "Commands" alone would
// drop "Interacting with Others" while still admitting them.
func TestGitReadsCommandSectionsAndNotDocumentationTopics(t *testing.T) {
	got := map[string]string{}
	for _, k := range gitTopLevel(gitHelpAll) {
		got[k.name] = k.desc
	}

	for _, want := range []string{"add", "worktree", "remote", "svn"} {
		if _, ok := got[want]; !ok {
			t.Errorf("%q missing; its section lists commands", want)
		}
	}
	for _, unwanted := range []string{"gitattributes", "gitignore"} {
		if _, ok := got[unwanted]; ok {
			t.Errorf("%q was read as a command; it is a documentation topic", unwanted)
		}
	}
	if got["remote"] != "Manage set of tracked repositories" {
		t.Errorf("remote description = %q, want the row's text", got["remote"])
	}
}

// The advice line above the first heading is unindented like a heading and must
// not open a section.
func TestGitsLeadingAdviceIsNotASectionHeading(t *testing.T) {
	if gitSectionListsCommands("See 'git help <command>' to read about a specific subcommand") {
		t.Error("the advice line opened a command section")
	}
}

// git names a subcommand's own subcommands only in the usage block, and writes
// its global flags into the middle of the path: `git remote [-v | --verbose]
// show <name>` puts a flag between the noun and the verb.
func TestGitReadsSubcommandsOutOfTheUsageBlock(t *testing.T) {
	help := `usage: git remote [-v | --verbose]
   or: git remote add [-t <branch>] [-m <master>] <name> <url>
   or: git remote rename [--[no-]progress] <old> <new>
   or: git remote [-v | --verbose] show [-n] <name>
   or: git remote set-url --add <name> <newurl>

    -v, --[no-]verbose    be verbose; must be placed before a subcommand
`
	var names []string
	for _, k := range gitSynopsisChildren([]string{"remote"}, help) {
		names = append(names, k.name)
	}

	want := "add,rename,show,set-url"
	if strings.Join(names, ",") != want {
		t.Errorf("subcommands = %v, want %v", names, want)
	}
}

// The first line repeats only the path and names no subcommand, so it must
// contribute nothing rather than a child named after a flag.
func TestAUsageLineRepeatingOnlyThePathNamesNoSubcommand(t *testing.T) {
	if name := gitNextWord([]string{"remote"}, strings.Fields("git remote [-v | --verbose]")); name != "" {
		t.Errorf("read %q as a subcommand of `git remote`", name)
	}
	if name := gitNextWord([]string{"worktree"}, strings.Fields("git worktree list [-v]")); name != "list" {
		t.Errorf("read %q, want list", name)
	}
}

// filter-branch sleeps ten seconds by design, which was the whole of git's read
// time. The command still appears; only its own screen is skipped.
func TestGitDoesNotReadFilterBranchsOwnHelp(t *testing.T) {
	name, _ := gitRecipe.Help("git", []string{"filter-branch"})
	if name != "" {
		t.Errorf("filter-branch would be read, costing ten seconds")
	}
	if name, args := gitRecipe.Help("git", []string{"worktree"}); name != "git" || strings.Join(args, " ") != "worktree -h" {
		t.Errorf("worktree -> (%q, %v), want git worktree -h", name, args)
	}
}

// tmux answers --help with 158 bytes of usage naming no commands. Its commands
// come from `tmux list-commands`, one per line with its argument spec.
func TestTmuxReadsListCommands(t *testing.T) {
	help := `attach-session [-dEfrx] [-c working-directory] [-t target-session]
bind-key [-nr] [-N note] [-T key-table] key command [argument ...]
list-commands [-F format] [command]
`
	kids := tmuxRecipe.Children(nil, help)
	if len(kids) != 3 {
		t.Fatalf("read %d commands, want 3: %v", len(kids), kids)
	}
	if kids[0].name != "attach-session" {
		t.Errorf("first command = %q, want attach-session", kids[0].name)
	}
	if !strings.HasPrefix(kids[0].desc, "[-dEfrx]") {
		t.Errorf("description = %q, want the argument spec", kids[0].desc)
	}
	// tmux has no per-command help to walk into.
	if name, _ := tmuxRecipe.Help("tmux", []string{"attach-session"}); name != "" {
		t.Error("tmux would be walked below its top level")
	}
}

// A recipe replaces the framework readers entirely, so the tool reports that it
// was read by one rather than claiming a shape it does not have.
func TestARecipeToolReportsItWasReadByOne(t *testing.T) {
	run := func(_ string, args ...string) string {
		switch strings.Join(args, " ") {
		case "help -a":
			return gitHelpAll
		case "add -h":
			return "usage: git add [<options>] [--] <pathspec>...\n"
		case "worktree -h":
			return "usage: git worktree add <path>\n   or: git worktree list\n"
		case "remote -h", "svn -h":
			return "usage: git thing\n"
		}
		return ""
	}
	tool, err := Extract("git", Options{Runner: run, MaxDepth: 2})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if tool.Framework != FrameworkRecipe {
		t.Errorf("framework = %q, want recipe", tool.Framework)
	}

	var paths []string
	tool.Walk(func(n *Node) { paths = append(paths, strings.Join(n.Path, " ")) })
	joined := strings.Join(paths, ",")
	for _, want := range []string{"worktree", "worktree add", "worktree list", "remote"} {
		if !strings.Contains(joined, want) {
			t.Errorf("%q missing from %v", want, paths)
		}
	}
}
