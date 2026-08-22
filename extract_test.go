package clisurface

import (
	"strings"
	"testing"
)

// fakeRunner answers from a map keyed by the joined arguments, so the parsers
// are tested against captured help output rather than whatever happens to be
// installed on the machine running the tests.
func fakeRunner(responses map[string]string) Runner {
	return func(_ string, args ...string) string {
		return responses[strings.Join(args, " ")]
	}
}

// extract reads a fake tool, failing the test rather than returning an error,
// so each case below reads as the parser assertion it is about.
func extract(t *testing.T, run Runner) *Tool {
	t.Helper()
	tool, err := Extract("demo", Options{Runner: run})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return tool
}

const cobraRoot = `A test tool.

Usage:
  demo [command]

Available Commands:
  books       Work with books
`

func TestCobraTreeComesFromCompleteNotHelp(t *testing.T) {
	run := fakeRunner(map[string]string{
		"__complete ":            "books\tWork with books\n:4\nCompletion ended with directive: ShellCompDirectiveNoFileComp",
		"__complete books ":      "list\tList books\nshow\tShow one book\n:4\nCompletion ended with directive: ShellCompDirectiveNoFileComp",
		"__complete books list ": ":4\nCompletion ended with directive: ShellCompDirectiveNoFileComp",
		"__complete books show ": ":4\nCompletion ended with directive: ShellCompDirectiveNoFileComp",
		"--help":                 cobraRoot,
		"books --help":           "Work with books\n\nUsage:\n  demo books [command]\n\nAvailable Commands:\n  list        List books\n  show        Show one book\n",
		"books list --help":      "List books\n\nUsage:\n  demo books list [flags]\n\nFlags:\n      --json   as JSON\n",
		"books show --help":      "Show one book\n\nUsage:\n  demo books show <id>\n",
	})

	tool := extract(t, run)
	if tool.Framework != FrameworkCobra {
		t.Fatalf("framework = %q, want cobra", tool.Framework)
	}

	var paths []string
	tool.Walk(func(n *Node) { paths = append(paths, strings.Join(n.Path, " ")) })
	want := []string{"books", "books list", "books show"}
	if strings.Join(paths, ",") != strings.Join(want, ",") {
		t.Errorf("paths = %v, want %v", paths, want)
	}
}

func TestCobraLeafCarriesItsFlags(t *testing.T) {
	run := fakeRunner(map[string]string{
		"__complete ":      "list\tList\n:4\nCompletion ended with directive: x",
		"__complete list ": ":4\nCompletion ended with directive: x",
		"--help":           "A test tool.\n\nUsage:\n  demo [command]\n\nAvailable Commands:\n  list        List\n",
		"list --help":      "Usage:\n  demo list [flags]\n\nFlags:\n      --json    as JSON\n      --limit int\n",
	})
	tool := extract(t, run)
	var flags []string
	tool.Walk(func(n *Node) { flags = n.Flags })
	if !contains(flags, "--json") || !contains(flags, "--limit") {
		t.Errorf("flags = %v, want --json and --limit", flags)
	}
}

// __complete answers with a leaf's ValidArgs when it has no subcommands, and
// says nothing about which kind of name it returned. A command accepting one of
// three fixed keywords offers those three, and they parse as three subcommands
// unless the help screen is consulted about which names are really commands.
func TestArgumentCompletionsAreNotSubcommands(t *testing.T) {
	run := fakeRunner(map[string]string{
		"__complete ":      "list\tList them\n:4\nCompletion ended with directive: x",
		"__complete list ": "checks\nmaintenance\nonetime\n:4\nCompletion ended with directive: x",
		"--help":           "Usage:\n  demo [command]\n\nAvailable Commands:\n  list        List them\n",
		// The help of a leaf that takes an argument names no commands at all.
		"list --help": "Usage:\n  demo list [category] [flags]\n\nFlags:\n  -h, --help   help for list\n",
	})
	tool := extract(t, run)

	var paths []string
	tool.Walk(func(n *Node) { paths = append(paths, strings.Join(n.Path, " ")) })
	if len(paths) != 1 || paths[0] != "list" {
		t.Errorf("paths = %v, want [list] — argument values were read as commands", paths)
	}
}

// A Rich "Arguments" panel is shaped exactly like a "Commands" panel. Reading
// one as subcommands recursed until a 12-command tool reported 1992 nodes.
func TestRichArgumentsPanelIsNotACommandList(t *testing.T) {
	root := "Usage: demo [OPTIONS] COMMAND\n" +
		"╭─ Commands ─────────╮\n" +
		"│ search   Search it │\n" +
		"╰────────────────────╯\n"
	search := "Usage: demo search [OPTIONS] QUERY\n" +
		"╭─ Arguments ────────╮\n" +
		"│ query    The query │\n" +
		"╰────────────────────╯\n" +
		"╭─ Options ──────────╮\n" +
		"│ --json   As JSON   │\n" +
		"╰────────────────────╯\n"

	run := fakeRunner(map[string]string{
		"--help":              root,
		"search --help":       search,
		"search query --help": search,
	})
	tool := extract(t, run)
	if tool.Framework != FrameworkRich {
		t.Fatalf("framework = %q, want rich", tool.Framework)
	}
	var n int
	tool.Walk(func(*Node) { n++ })
	if n != 1 {
		t.Errorf("walked %d nodes, want 1 — an Arguments panel was read as commands", n)
	}
}

const sectionHelp = `
 demo

Commands
───────────────────────
  backup [--tag <name>]  Copy the paths into today's snapshot
  snapshots              List the snapshots
  sync push              Force push
  sync status            Show sync status
  install                Install everything
  install --check        Show what is missing

Options
──────────────────────
  -c, --config <name>  Config to use
`

func TestSectionRowsKeepArgsOutAndNestMultiWord(t *testing.T) {
	run := fakeRunner(map[string]string{"--help": sectionHelp})
	tool := extract(t, run)
	if tool.Framework != FrameworkSection {
		t.Fatalf("framework = %q, want section", tool.Framework)
	}

	got := map[string]string{}
	tool.Walk(func(n *Node) { got[strings.Join(n.Path, " ")] = n.Short })

	if _, ok := got["backup"]; !ok {
		t.Error("backup missing — an argument spec after the name swallowed the row")
	}
	if _, ok := got["sync push"]; !ok {
		t.Errorf("sync push missing; got %v", keys(got))
	}
	if got["install"] != "Install everything" {
		t.Errorf("install short = %q, want the first row's text, not the --check row's", got["install"])
	}
	for path := range got {
		if strings.Contains(path, "--") {
			t.Errorf("%q parsed as a command; a flag row is not a subcommand", path)
		}
	}
}

// A tool with no per-command help answers `tool sub --help` with the root
// screen, which made every command list its siblings at every level.
func TestIdenticalChildHelpStopsTheWalk(t *testing.T) {
	root := "Usage: demo [command]\n\nAvailable Commands:\n  alpha   A\n  beta    B\n"
	run := fakeRunner(map[string]string{
		"__complete ":            "alpha\tA\nbeta\tB\n:4\nCompletion ended with directive: x",
		"__complete alpha ":      "alpha\tA\nbeta\tB\n:4\nCompletion ended with directive: x",
		"__complete beta ":       "alpha\tA\nbeta\tB\n:4\nCompletion ended with directive: x",
		"__complete alpha beta ": "alpha\tA\nbeta\tB\n:4\nCompletion ended with directive: x",
		"__complete beta alpha ": "alpha\tA\nbeta\tB\n:4\nCompletion ended with directive: x",
		"--help":                 root,
		"alpha --help":           root,
		"beta --help":            root,
	})
	tool := extract(t, run)
	var n int
	tool.Walk(func(*Node) { n++ })
	if n != 2 {
		t.Errorf("walked %d nodes, want 2 — root help echoed as each child's help", n)
	}
}

func TestFlatToolHasNoChildren(t *testing.T) {
	run := fakeRunner(map[string]string{"--help": "Usage: demo [OPTIONS]\n\nOptions:\n  --clean  Clean\n"})
	tool := extract(t, run)
	if tool.Framework != FrameworkFlat {
		t.Errorf("framework = %q, want flat", tool.Framework)
	}
	var n int
	tool.Walk(func(*Node) { n++ })
	if n != 0 {
		t.Errorf("walked %d nodes, want 0", n)
	}
}

// clapHostile carries every variation measured across uv, cargo, rustup and
// ruff: a four-space indent, aliases after the name, the block sitting below
// Options: rather than first, a wrapped description continuation, a generated
// `help` row, and a further block after it.
const clapHostile = `A test tool.

Usage: demo [OPTIONS] [COMMAND]

Options:
  -V, --version    Print version info and exit

Commands:
    build, b    Compile the current package
    check, c    Analyze the current package and report errors, but don't build
                object files
    tool        Run and install commands
    help        Print this message or the help of the given subcommand(s)

Cache options:
  -n, --no-cache    Avoid the cache
`

// clapNested indents by two where the root indents by four, which is the
// disagreement between uv and cargo inside one tree.
const clapNested = `Run and install commands

Usage: demo tool [OPTIONS] <COMMAND>

Commands:
  run     Run a command provided by a package
  list    List installed tools

Global options:
  -q, --quiet  Silence
`

func TestClapReadsBothIndentsAliasesAndNesting(t *testing.T) {
	run := fakeRunner(map[string]string{
		"--help":           clapHostile,
		"build --help":     "Compile the current package\n\nUsage: demo build [OPTIONS]\n\nOptions:\n      --release  Build optimized\n",
		"check --help":     "Analyze the current package\n\nUsage: demo check [OPTIONS]\n",
		"tool --help":      clapNested,
		"tool run --help":  "Run a command\n\nUsage: demo tool run <NAME>\n",
		"tool list --help": "List installed tools\n\nUsage: demo tool list\n",
	})

	tool := extract(t, run)
	if tool.Framework != FrameworkClap {
		t.Fatalf("framework = %q, want clap", tool.Framework)
	}

	var paths []string
	tool.Walk(func(n *Node) { paths = append(paths, strings.Join(n.Path, " ")) })
	want := []string{"build", "check", "tool", "tool run", "tool list"}
	if strings.Join(paths, ",") != strings.Join(want, ",") {
		t.Errorf("paths = %v, want %v", paths, want)
	}

	// `build, b` names one command, not two, and the alias is not one of them.
	if contains(paths, "b") || contains(paths, "c") {
		t.Errorf("an alias was read as a command: %v", paths)
	}
	// The wrapped second line of check's description is indented like a row.
	if contains(paths, "object") || contains(paths, "object files") {
		t.Errorf("a wrapped description line was read as a command: %v", paths)
	}
}

func TestClapDescriptionsSurviveTheAlias(t *testing.T) {
	run := fakeRunner(map[string]string{
		"--help":       clapHostile,
		"build --help": "Usage: demo build\n",
		"check --help": "Usage: demo check\n",
		"tool --help":  "Usage: demo tool\n",
	})
	tool := extract(t, run)
	got := map[string]string{}
	tool.Walk(func(n *Node) { got[n.Name] = n.Short })
	if got["build"] != "Compile the current package" {
		t.Errorf("build short = %q, want the description after the alias", got["build"])
	}
}

// A name that is not on PATH prints nothing. Reading that as a tool with no
// subcommands is the failure worth refusing: the result is well formed, exits
// zero, and confidently describes something that does not exist.
func TestSilentBinaryIsAnErrorNotAnEmptyTree(t *testing.T) {
	_, err := Extract("demo", Options{Runner: fakeRunner(nil)})
	if err == nil {
		t.Fatal("Extract returned no error for a binary that printed nothing")
	}
	if !strings.Contains(err.Error(), "demo") {
		t.Errorf("error = %q, want it to name the binary", err)
	}
}

func TestBodyIsKeptOnlyWhenAsked(t *testing.T) {
	responses := map[string]string{
		"__complete ":      "list\tList\n:4\nCompletion ended with directive: x",
		"__complete list ": ":4\nCompletion ended with directive: x",
		"--help":           "A test tool.\n\nUsage:\n  demo [command]\n\nAvailable Commands:\n  list        List\n",
		"list --help":      "List things.\n\nUsage:\n  demo list [flags]\n",
	}

	off := extract(t, fakeRunner(responses))
	if off.Root.Body != "" {
		t.Errorf("root carries a body with WithBody unset")
	}
	off.Walk(func(n *Node) {
		if n.Body != "" {
			t.Errorf("%v carries a body with WithBody unset", n.Path)
		}
	})

	on, err := Extract("demo", Options{Runner: fakeRunner(responses), WithBody: true})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if on.Root.Body != responses["--help"] {
		t.Errorf("root body = %q, want the whole help screen", on.Root.Body)
	}
	on.Walk(func(n *Node) {
		if n.Body == "" {
			t.Errorf("%v carries no body with WithBody set", n.Path)
		}
	})
}

func TestMaxDepthBoundsTheWalk(t *testing.T) {
	responses := map[string]string{
		"__complete ":       "a\tA\n:4\nCompletion ended with directive: x",
		"__complete a ":     "b\tB\n:4\nCompletion ended with directive: x",
		"__complete a b ":   "c\tC\n:4\nCompletion ended with directive: x",
		"__complete a b c ": ":4\nCompletion ended with directive: x",
		"--help":            "Usage:\n  demo [command]\n\nAvailable Commands:\n  a    A\n",
		"a --help":          "Usage:\n  demo a [command]\n\nAvailable Commands:\n  b    B\n",
		"a b --help":        "Usage:\n  demo a b [command]\n\nAvailable Commands:\n  c    C\n",
		"a b c --help":      "Usage:\n  demo a b c [flags]\n",
	}

	for _, tc := range []struct {
		name  string
		depth int
		want  int
	}{
		{"zero takes the default and reaches every node", 0, 3},
		{"one stops after the first level", 1, 1},
		{"two stops after the second", 2, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tool, err := Extract("demo", Options{Runner: fakeRunner(responses), MaxDepth: tc.depth})
			if err != nil {
				t.Fatalf("Extract: %v", err)
			}
			var n int
			tool.Walk(func(*Node) { n++ })
			if n != tc.want {
				t.Errorf("walked %d nodes at MaxDepth %d, want %d", n, tc.depth, tc.want)
			}
		})
	}
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
