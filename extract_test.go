package clisurface

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
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

// recordingRunner answers like fakeRunner and keeps every argument list it was
// asked for, so a test can assert about a command that was never run.
func recordingRunner(responses map[string]string, seen *[]string) Runner {
	return func(_ string, args ...string) string {
		key := strings.Join(args, " ")
		*seen = append(*seen, key)
		return responses[key]
	}
}

// A tool that lists no commands must never be handed __complete. The argument
// is not inert on a tool that takes free text: wl-copy read it as clipboard
// content, wrote it, and daemonized holding the pipe.
func TestProbeIsNeverRunOnAToolThatListsNoCommands(t *testing.T) {
	var seen []string
	run := recordingRunner(map[string]string{
		"--help": "Usage: demo [options] text...\n\nOptions:\n  -p, --primary  Use the primary selection\n",
	}, &seen)

	tool := extract(t, run)
	if tool.Framework != FrameworkFlat {
		t.Errorf("framework = %q, want flat", tool.Framework)
	}
	for _, args := range seen {
		if strings.HasPrefix(args, "__complete") {
			t.Fatalf("probed with %q; a tool listing no commands must not be probed (ran: %v)", args, seen)
		}
	}
}

// gh heads its list "CORE COMMANDS" with no colon and is still cobra, so the
// gate cannot require the colon that isCommandHeading does.
func TestHeadingWithoutAColonStillEarnsTheProbe(t *testing.T) {
	run := fakeRunner(map[string]string{
		"--help":           "USAGE\n  demo <command>\n\nCORE COMMANDS\n  auth:  Authenticate\n",
		"__complete ":      "auth\tAuthenticate\n:4\nCompletion ended with directive: x",
		"__complete auth ": ":4\nCompletion ended with directive: x",
		"auth --help":      "Authenticate\n\nUSAGE\n  demo auth\n",
	})

	tool := extract(t, run)
	if tool.Framework != FrameworkCobra {
		t.Fatalf("framework = %q, want cobra", tool.Framework)
	}
}

// A row is not a heading. npm writes "npm -l   display usage info for all
// commands" unindented, which wears the suffix the gate looks for.
func TestACommandRowIsNotACommandHeading(t *testing.T) {
	if listsCommands("Usage: demo <command>\n\nnpm -l   display usage info for all commands\n") {
		t.Error("a row ending in \"commands\" was read as a command heading")
	}
	if !listsCommands("Usage: demo\n\nCommands:\n  go   Go\n") {
		t.Error("a bare \"Commands:\" heading was not read as one")
	}
	// sesh centers "COMMANDS" inside a styled box and is still cobra, so the
	// gate cannot require column zero the way isCommandHeading does.
	if !listsCommands("Usage: demo\n\n  COMMANDS  \n    list   List them\n") {
		t.Error("an indented heading was rejected; sesh reads flat without it")
	}
	if listsCommands("Usage: demo\n\n  metadata  Metadata related commands:\n") {
		t.Error("a row with a description gutter was read as a heading")
	}
}

// Extract runs the root --help to detect the framework, and the walk must not
// run it again. A second read doubles the cost of every tool and doubles any
// side effect it has: claude-desktop ignores --help and launches Electron, so
// it was launched twice and took 40s to abandon instead of 20s.
func TestTheRootHelpIsReadOnce(t *testing.T) {
	responses := map[string]string{
		"--help":      "Usage:\n  demo [command]\n\nAvailable Commands:\n  list   List them\n",
		"list --help": "Usage:\n  demo list [flags]\n",
	}
	var mu sync.Mutex
	calls := map[string]int{}
	run := func(_ string, args ...string) string {
		key := strings.Join(args, " ")
		mu.Lock()
		calls[key]++
		mu.Unlock()
		return responses[key]
	}

	if _, err := Extract("demo", Options{Runner: run}); err != nil {
		t.Fatalf("Extract: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if calls["--help"] != 1 {
		t.Errorf("ran the root --help %d times, want 1", calls["--help"])
	}
	if calls["list --help"] != 1 {
		t.Errorf("ran `list --help` %d times, want 1", calls["list --help"])
	}
}

// zk groups its commands under bare unindented labels inside the block. Ending
// the block at any unindented line dropped every command underneath them, so it
// read as a tool with no commands at all.
func TestABareLabelDoesNotEndACommandBlock(t *testing.T) {
	help := "Usage: demo <command>\n\n" +
		"Commands:\n\n" +
		"NOTEBOOK\n  A notebook is a directory\n\n" +
		"  init      Create a notebook\n" +
		"  index     Index the notes\n\n" +
		"NOTES\n  Edit or browse your notes\n\n" +
		"  new       Create a note\n" +
		"  list      List notes\n\n" +
		"Flags:\n" +
		"  -h, --help   Show help\n"

	run := fakeRunner(map[string]string{
		"--help":       help,
		"init --help":  "Usage: demo init\n",
		"index --help": "Usage: demo index\n",
		"new --help":   "Usage: demo new\n",
		"list --help":  "Usage: demo list\n",
	})
	tool := extract(t, run)

	var got []string
	tool.Walk(func(n *Node) { got = append(got, n.Name) })
	want := "init,index,new,list"
	if strings.Join(got, ",") != want {
		t.Errorf("commands = %v, want %v", got, want)
	}
}

// "Flags:" ends the block and its rows are not commands. Without the colon
// test, everything under it would be read as one.
func TestASectionHeadingStillEndsACommandBlock(t *testing.T) {
	help := "Usage: demo <command>\n\n" +
		"Commands:\n" +
		"  run       Run it\n\n" +
		"Options:\n" +
		"  verbose   Be loud\n"

	run := fakeRunner(map[string]string{
		"--help":     help,
		"run --help": "Usage: demo run\n",
	})
	tool := extract(t, run)

	var got []string
	tool.Walk(func(n *Node) { got = append(got, n.Name) })
	if strings.Join(got, ",") != "run" {
		t.Errorf("commands = %v, want [run] — an Options row was read as a command", got)
	}
}

// argparse writes "positional arguments:" for a subcommand set and for a plain
// argument alike. The brace-wrapped choices are the only thing separating them,
// so a tool taking one positional must not read as a tool with one subcommand.
func TestArgparseReadsSubparsersAndNotAPlainPositional(t *testing.T) {
	root := "usage: demo [-h] {run,clean} ...\n\n" +
		"positional arguments:\n" +
		"  {run,clean}\n" +
		"    run                 Run hooks.\n" +
		"    clean               Clean out files.\n" +
		"    help                Show help for a specific command.\n" +
		"\n" +
		"options:\n" +
		"  -h, --help            show this help message and exit\n"

	// A leaf naming one argument, which is the shape that must not recurse.
	run := "usage: demo run [-h] [hook]\n\n" +
		"positional arguments:\n" +
		"  hook                  A single hook-id to run\n"

	tool := extract(t, fakeRunner(map[string]string{
		"--help":       root,
		"run --help":   run,
		"clean --help": "usage: demo clean [-h]\n",
	}))
	if tool.Framework != FrameworkArgparse {
		t.Fatalf("framework = %q, want argparse", tool.Framework)
	}

	var got []string
	tool.Walk(func(n *Node) { got = append(got, strings.Join(n.Path, " ")) })
	want := "run,clean"
	if strings.Join(got, ",") != want {
		t.Errorf("commands = %v, want %v — \"help\" is generated, and hook is an argument", got, want)
	}
}

// wideCobra builds a root with n leaf children, so a concurrency bound has
// something to be exceeded on.
func wideCobra(n int) map[string]string {
	responses := map[string]string{}
	var rows, complete strings.Builder
	for i := range n {
		name := fmt.Sprintf("cmd%02d", i)
		fmt.Fprintf(&rows, "  %s   Command %d\n", name, i)
		fmt.Fprintf(&complete, "%s\tCommand %d\n", name, i)
		responses[name+" --help"] = "Usage:\n  demo " + name + " [flags]\n"
		responses["__complete "+name+" "] = ":4\nCompletion ended with directive: x"
	}
	responses["--help"] = "Usage:\n  demo [command]\n\nAvailable Commands:\n" + rows.String()
	responses["__complete "] = complete.String() + ":4\nCompletion ended with directive: x"
	return responses
}

func TestConcurrencyIsBounded(t *testing.T) {
	for _, limit := range []int{1, 4} {
		t.Run(fmt.Sprintf("limit %d", limit), func(t *testing.T) {
			responses := wideCobra(24)
			var inFlight, peak atomic.Int64

			run := func(_ string, args ...string) string {
				n := inFlight.Add(1)
				defer inFlight.Add(-1)
				for {
					p := peak.Load()
					if n <= p || peak.CompareAndSwap(p, n) {
						break
					}
				}
				// Widen the window so genuine overlap is observable rather than
				// depending on the scheduler happening to interleave.
				time.Sleep(time.Millisecond)
				return responses[strings.Join(args, " ")]
			}

			if _, err := Extract("demo", Options{Runner: run, Concurrency: limit}); err != nil {
				t.Fatalf("Extract: %v", err)
			}
			if got := peak.Load(); got > int64(limit) {
				t.Errorf("peak in-flight = %d, want at most %d", got, limit)
			}
			// Without this the bound would pass trivially on a serial walk.
			if limit > 1 && peak.Load() < 2 {
				t.Errorf("peak in-flight = %d; nothing ran concurrently", peak.Load())
			}
		})
	}
}

// Children are written from separate goroutines into a pre-sized slice, so the
// order they end up in must not depend on which finished first.
func TestChildOrderSurvivesConcurrency(t *testing.T) {
	responses := wideCobra(16)
	var want string
	for run := range 20 {
		tool, err := Extract("demo", Options{Runner: fakeRunner(responses)})
		if err != nil {
			t.Fatalf("Extract: %v", err)
		}
		var paths []string
		tool.Walk(func(n *Node) { paths = append(paths, strings.Join(n.Path, " ")) })
		got := strings.Join(paths, ",")
		if run == 0 {
			want = got
			if !strings.HasPrefix(got, "cmd00,cmd01,cmd02") {
				t.Fatalf("order = %q, want the help screen's order", got)
			}
			continue
		}
		if got != want {
			t.Fatalf("run %d order = %q, want %q", run, got, want)
		}
	}
}

// headingHostile carries every variation measured across uv, cargo, rustup and
// ruff: a four-space indent, aliases after the name, the block sitting below
// Options: rather than first, a wrapped description continuation, a generated
// `help` row, and a further block after it.
const headingHostile = `A test tool.

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

// headingNested indents by two where the root indents by four, which is the
// disagreement between uv and cargo inside one tree.
const headingNested = `Run and install commands

Usage: demo tool [OPTIONS] <COMMAND>

Commands:
  run     Run a command provided by a package
  list    List installed tools

Global options:
  -q, --quiet  Silence
`

func TestClapReadsBothIndentsAliasesAndNesting(t *testing.T) {
	run := fakeRunner(map[string]string{
		"--help":           headingHostile,
		"build --help":     "Compile the current package\n\nUsage: demo build [OPTIONS]\n\nOptions:\n      --release  Build optimized\n",
		"check --help":     "Analyze the current package\n\nUsage: demo check [OPTIONS]\n",
		"tool --help":      headingNested,
		"tool run --help":  "Run a command\n\nUsage: demo tool run <NAME>\n",
		"tool list --help": "List installed tools\n\nUsage: demo tool list\n",
	})

	tool := extract(t, run)
	if tool.Framework != FrameworkHeading {
		t.Fatalf("framework = %q, want heading", tool.Framework)
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

// A tool may split its command list under headings of its own, and the split is
// presentation rather than structure — every block holds commands.
//
// The last row is the guard: its description ends in "commands:", and an
// unanchored suffix match would read it as opening a block, swallowing the
// options below it as commands.
const headingSplitBlocks = `Usage: demo [global options] <subcommand> [args]

Main commands:
  init      Prepare your working directory
  apply     Create or update infrastructure

All other commands:
  fmt       Reformat your configuration
  metadata  Metadata related commands:

Global options:
  -chdir=DIR  Switch to a different directory
`

func TestEveryCommandBlockIsRead(t *testing.T) {
	responses := map[string]string{"--help": headingSplitBlocks}
	for _, name := range []string{"init", "apply", "fmt", "metadata"} {
		responses[name+" --help"] = "Usage: demo " + name + " [options]\n"
	}

	tool := extract(t, fakeRunner(responses))
	if tool.Framework != FrameworkHeading {
		t.Fatalf("framework = %q, want heading", tool.Framework)
	}

	var paths []string
	tool.Walk(func(n *Node) { paths = append(paths, strings.Join(n.Path, " ")) })
	want := []string{"init", "apply", "fmt", "metadata"}
	if strings.Join(paths, ",") != strings.Join(want, ",") {
		t.Errorf("paths = %v, want %v — both blocks, in screen order", paths, want)
	}
	if contains(paths, "-chdir=DIR") || len(paths) > len(want) {
		t.Errorf("a row ending in \"commands:\" opened a block: %v", paths)
	}
}

// A tool with too many commands to describe individually prints them as a
// comma-separated run, wrapped, with no descriptions.
//
// Three guards are in the fixture. The Usage block above holds rows shaped
// exactly like command rows and must not be read, because no heading opened it.
// The run carries `help`, which is generated rather than the tool's own. And
// the last row is not a list at all — one of its tokens has a space — so it is
// refused whole rather than contributing the tokens that looked right.
const headingCommaRun = `demo <command>

Usage:

demo install        install all the dependencies
demo test           run this project's tests

All commands:

    access, adduser, audit, bugs,
    cache, help, config
    run these, with care

Specify configs in the ini-formatted file:
    ~/.config/demo/rc
`

func TestCommandsListedAsACommaRun(t *testing.T) {
	responses := map[string]string{"--help": headingCommaRun}
	for _, name := range []string{"access", "adduser", "audit", "bugs", "cache", "config"} {
		responses[name+" --help"] = "Usage: demo " + name + "\n"
	}

	tool := extract(t, fakeRunner(responses))
	var paths []string
	tool.Walk(func(n *Node) { paths = append(paths, strings.Join(n.Path, " ")) })

	want := []string{"access", "adduser", "audit", "bugs", "cache", "config"}
	if strings.Join(paths, ",") != strings.Join(want, ",") {
		t.Errorf("paths = %v, want %v", paths, want)
	}
	for _, unwanted := range []string{"install", "test", "help", "run", "these", "with care"} {
		if contains(paths, unwanted) {
			t.Errorf("%q was read as a command: %v", unwanted, paths)
		}
	}
}

func TestClapDescriptionsSurviveTheAlias(t *testing.T) {
	run := fakeRunner(map[string]string{
		"--help":       headingHostile,
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

// A consumer that renders a help screen wants the color the tool emitted, and
// the parsers must not care either way. The Runner returns what the tool
// printed; every reader normalizes; only Body keeps it.
func TestColorDoesNotReachTheParsersButDoesReachTheBody(t *testing.T) {
	clean := map[string]string{
		"__complete ":      "list\tList\n:4\nCompletion ended with directive: x",
		"__complete list ": ":4\nCompletion ended with directive: x",
		"--help":           "A test tool.\n\nUsage:\n  demo [command]\n\nAvailable Commands:\n  list        List\n",
		"list --help":      "List things.\n\nUsage:\n  demo list [flags]\n",
	}
	// Color every response, including the completion callback, so the readers
	// are proven rather than merely unexercised.
	colored := map[string]string{}
	for k, v := range clean {
		colored[k] = strings.ReplaceAll(v, "list", "\x1b[1;36mlist\x1b[0m")
	}

	paths := func(tool *Tool) string {
		var got []string
		tool.Walk(func(n *Node) { got = append(got, strings.Join(n.Path, " ")) })
		return strings.Join(got, ",")
	}

	plain := extract(t, fakeRunner(clean))
	fancy, err := Extract("demo", Options{Runner: fakeRunner(colored), WithBody: true})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	if paths(plain) != paths(fancy) {
		t.Errorf("colored tree = %q, want the same as plain %q", paths(fancy), paths(plain))
	}
	if !strings.Contains(fancy.Root.Body, "\x1b[") {
		t.Error("root body lost the color the tool printed")
	}
	fancy.Walk(func(n *Node) {
		if n.Short != "List" {
			t.Errorf("%v short = %q, want the description without escapes", n.Path, n.Short)
		}
	})
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

// oclif heads its list "COMMANDS" in caps with no colon, and decorates each row
// with a shell prompt. Ten tools in a corpus of popular npm CLIs read as flat
// while the colon was required — eas, netlify, ntl, nypm and ipx among them.
func TestAnOclifScreenIsRead(t *testing.T) {
	root := "USAGE\n  $ demo [COMMAND]\n\n" +
		"COMMANDS\n" +
		"  $ blobs       Manage objects\n" +
		"  $ build       Build locally\n"
	run := fakeRunner(map[string]string{
		"--help":       root,
		"blobs --help": "USAGE\n  $ demo blobs\n",
		"build --help": "USAGE\n  $ demo build\n",
	})

	tool := extract(t, run)
	var got []string
	tool.Walk(func(n *Node) { got = append(got, n.Name) })
	if strings.Join(got, ",") != "blobs,build" {
		t.Errorf("commands = %v, want blobs,build", got)
	}
}

// yargs repeats the whole invocation in every row, so the name parsed as three
// words and was refused. nyc, metro, newman and cdk all read as flat for it.
func TestARowRepeatingTheBinaryAndItsArgumentsIsRead(t *testing.T) {
	root := "Commands:\n" +
		"  demo instrument <input> [output]  instruments a file\n" +
		"  demo check-coverage               check thresholds\n"
	run := fakeRunner(map[string]string{
		"--help":                root,
		"instrument --help":     "usage: demo instrument\n",
		"check-coverage --help": "usage: demo check-coverage\n",
	})

	tool := extract(t, run)
	got := map[string]string{}
	tool.Walk(func(n *Node) { got[n.Name] = n.Short })
	if _, ok := got["instrument"]; !ok {
		t.Errorf("instrument missing; got %v", keys(got))
	}
	if got["check-coverage"] != "check thresholds" {
		t.Errorf("description = %q, want the row's text", got["check-coverage"])
	}
}

// An argument spec after the name is not part of the name. playerctl writes
// "position [OFFSET][+/-]" and lost six of its thirteen commands to it, while a
// genuinely two-word command must still arrive whole.
func TestAnArgumentSpecIsNotPartOfTheCommandName(t *testing.T) {
	cases := []struct{ row, want string }{
		{"position [OFFSET][+/-]", "position"},
		{"metadata [KEY...]", "metadata"},
		{"open [URI]", "open"},
		{"sync push", "sync push"},
		{"demo instrument <input>", "instrument"},
		{"$ blobs", "blobs"},
		{"--json", ""},
	}
	for _, c := range cases {
		if got := commandFromRow(c.row, "demo"); got != c.want {
			t.Errorf("commandFromRow(%q) = %q, want %q", c.row, got, c.want)
		}
	}
}

// The colon became optional, so the gutter is the only thing left separating a
// heading from a row that happens to end in "commands".
func TestARowEndingInCommandsIsStillNotAHeading(t *testing.T) {
	for _, row := range []string{
		"metadata  Metadata related commands:",
		"npm -l             display usage info for all commands",
	} {
		if isCommandHeading(row, strings.TrimSpace(row)) {
			t.Errorf("%q opened a command block", row)
		}
	}
	for _, heading := range []string{"COMMANDS", "Commands:", "Available Commands:", "CORE COMMANDS"} {
		if !isCommandHeading(heading, heading) {
			t.Errorf("%q did not open a command block", heading)
		}
	}
}
