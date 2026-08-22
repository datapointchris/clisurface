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

// aws drives its completer through env(1) because Runner passes no environment
// and aws_completer reads COMP_LINE rather than argv. The line is what a shell
// would present: the words typed so far and a trailing space, cursor at the end.
func TestAwsAsksItsCompleterTheWayAShellWould(t *testing.T) {
	name, args := awsRecipe.Help("aws", nil)
	if name != "env" {
		t.Errorf("root runs %q, want env", name)
	}
	want := "COMP_LINE=aws  COMP_POINT=4 aws_completer"
	if got := strings.Join(args, " "); got != want {
		t.Errorf("root args = %q, want %q", got, want)
	}

	_, args = awsRecipe.Help("aws", []string{"s3"})
	want = "COMP_LINE=aws s3  COMP_POINT=7 aws_completer"
	if got := strings.Join(args, " "); got != want {
		t.Errorf("s3 args = %q, want %q", got, want)
	}

	// Below an operation the completer answers with flags, so it is not asked.
	if name, _ := awsRecipe.Help("aws", []string{"ec2", "describe-instances"}); name != "" {
		t.Errorf("aws would be asked below an operation, and gets flags back")
	}
}

// A completion reply below an operation is flags. Reading those as commands
// would turn every operation into a node with a --dash child.
func TestAwsKeepsFlagsOutOfTheTree(t *testing.T) {
	reply := "--instance-ids\n--include-managed-resources\ndescribe-instances\n\ndescribe-instances\n"
	var names []string
	for _, k := range awsRecipe.Children(nil, reply) {
		names = append(names, k.name)
	}
	if strings.Join(names, ",") != "describe-instances" {
		t.Errorf("children = %v, want only describe-instances", names)
	}
}

// A surface too large to read whole names its children without reading them.
// Reading all 438 of aws's services costs twelve seconds to show one screen.
func TestALazyRecipeNamesChildrenWithoutReadingThem(t *testing.T) {
	var asked []string
	run := func(_ string, args ...string) string {
		line := ""
		for _, a := range args {
			if strings.HasPrefix(a, "COMP_LINE=") {
				line = strings.TrimPrefix(a, "COMP_LINE=")
			}
		}
		asked = append(asked, line)
		if line == "aws " {
			return "s3\nec2\n"
		}
		return "ls\ncp\n"
	}

	tool, err := Extract("aws", Options{Runner: run})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(tool.Root.Children) != 2 {
		t.Fatalf("read %d services, want 2", len(tool.Root.Children))
	}
	for _, c := range tool.Root.Children {
		if !c.Unread {
			t.Errorf("%q was read up front; it must be named and left unread", c.Name)
		}
		if len(c.Children) != 0 {
			t.Errorf("%q carries children it should not have read", c.Name)
		}
	}
	if len(asked) != 1 {
		t.Errorf("asked the completer %d times, want 1: %v", len(asked), asked)
	}
}

// ExtractAt is the moment the caller asked for those children, so it must not
// stop again where the first walk stopped.
func TestExtractAtReadsWhatTheFirstWalkLeftUnread(t *testing.T) {
	run := func(_ string, args ...string) string {
		for _, a := range args {
			if a == "COMP_LINE=aws s3 " {
				return "ls\ncp\nsync\n"
			}
		}
		return ""
	}

	node, err := ExtractAt("aws", []string{"s3"}, Options{Runner: run})
	if err != nil {
		t.Fatalf("ExtractAt: %v", err)
	}
	if node.Name != "s3" {
		t.Errorf("name = %q, want s3", node.Name)
	}
	var names []string
	for _, c := range node.Children {
		names = append(names, c.Name)
	}
	if strings.Join(names, ",") != "ls,cp,sync" {
		t.Errorf("operations = %v, want ls,cp,sync", names)
	}
}

// direnv writes its rows unindented with an argument spec and a trailing colon,
// and several aliases share one description written underneath them.
func TestDirenvSharesOneDescriptionAcrossAnAliasGroup(t *testing.T) {
	help := "Usage: direnv COMMAND [...ARGS]\n\n" +
		"Available commands\n" +
		"------------------\n" +
		"allow [PATH_TO_RC]:\n" +
		"permit [PATH_TO_RC]:\n" +
		"  Grants direnv permission to load the given .envrc file.\n" +
		"edit [PATH_TO_RC]:\n" +
		"  Opens PATH_TO_RC into an $EDITOR.\n"

	kids := direnvRecipe.Children(nil, help)
	got := map[string]string{}
	for _, k := range kids {
		got[k.name] = k.desc
	}
	if len(got) != 3 {
		t.Fatalf("commands = %v, want allow, permit and edit", got)
	}
	if got["allow"] != got["permit"] || got["allow"] == "" {
		t.Errorf("the alias group did not share its description: %v", got)
	}
	if got["edit"] == got["allow"] {
		t.Error("edit took the group's description instead of its own")
	}
}

// direnv reads --help as a path, so `direnv allow --help` does not print
// anything — it tries to allow a file called "--help". The walk must never
// descend, which is why direnv is a recipe rather than a reader.
func TestDirenvIsNeverWalkedBelowItsTopLevel(t *testing.T) {
	if name, _ := direnvRecipe.Help("direnv", []string{"allow"}); name != "" {
		t.Errorf("direnv would run `allow --help`, which authorizes a file called --help")
	}
	if name, args := direnvRecipe.Help("direnv", nil); name != "direnv" || strings.Join(args, " ") != "--help" {
		t.Errorf("root -> (%q, %v), want direnv --help", name, args)
	}
}
