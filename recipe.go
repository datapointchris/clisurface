package clisurface

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// FrameworkRecipe marks a tree read through a hand-written [Recipe] rather than
// by recognizing a framework. The shape is that one tool's own, which is why it
// needed a recipe.
const FrameworkRecipe Framework = "recipe"

// A Recipe is how to read one tool whose help no reader can reach.
//
// It names the way in and never the commands themselves. A list of commands
// would be stale the day the tool released; an entry point outlives that, so
// `git switch` appears the day git ships it without anything here changing.
//
// The two the walk assumes are both wrong for some real tool. `--help` is an
// error on aws, and on git it opens a man page below the top level. So a recipe
// supplies the invocation and the reader together — knowing how to ask is
// useless without knowing how to read what comes back.
type Recipe struct {
	// Help returns what to run for one node. The path is the command words
	// below the binary, empty at the root. Returning an empty name means this
	// node cannot be read and has no children.
	//
	// It returns a binary as well as arguments because a tool's commands are
	// not always listed by the tool: aws lists its services through
	// aws_completer, a separate program. No environment is returned, because
	// [Runner] takes none — git needs none either, since it skips its pager
	// when stdout is a pipe.
	Help func(binary string, path []string) (name string, args []string)

	// Children reads one node's help into the commands below it.
	Children func(path []string, help string) []child

	// MaxDepth bounds this tool alone, for a surface too large to walk whole.
	// Zero leaves [Options].MaxDepth in charge.
	MaxDepth int

	// LazyBelow is the depth at which children stop being read up front. A node
	// at or below it is returned with [Node].Unread set and no children, for a
	// caller to fill with [ExtractAt] when someone actually looks at it.
	//
	// Zero disables it and the whole tree is read. aws sets 1: reading every
	// service costs 438 calls and twelve seconds, and reading one costs 175ms,
	// so a person browsing three services pays half a second rather than twelve.
	LazyBelow int
}

// recipes is the table, keyed by binary name.
//
// Every entry earns its place by being unreachable otherwise. A tool a reader
// already handles must not appear here, because a recipe is frozen knowledge
// and a reader is not.
var recipes = map[string]Recipe{
	"aws":  awsRecipe,
	"git":  gitRecipe,
	"tmux": tmuxRecipe,
}

// recipeFor returns the recipe for a binary, if the table holds one.
func recipeFor(binary string) (Recipe, bool) {
	r, ok := recipes[binary]
	return r, ok
}

// Recipes names every tool read by a hand-written recipe, so a consumer can say
// which tools it treats specially rather than leaving a person to guess.
func Recipes() []string {
	names := make([]string, 0, len(recipes))
	for name := range recipes {
		names = append(names, name)
	}
	return names
}

// extractByRecipe walks a tool through its recipe rather than through a
// framework reader.
//
// Kept apart from the framework walk instead of folded into it. The two agree
// on almost nothing — not how help is asked for, not which program answers, not
// how the answer is read — so a shared function would be a switch at every step
// with one branch never taken.
func extractByRecipe(binary string, r Recipe, opts Options, run Runner) (*Tool, error) {
	name, args := r.Help(binary, nil)
	if name == "" {
		return nil, fmt.Errorf("clisurface: the recipe for %q reads nothing at its root", binary)
	}
	rootRaw := run(name, args...)
	if strings.TrimSpace(stripANSI(rootRaw)) == "" {
		return nil, fmt.Errorf("clisurface: %q printed no help; is it on PATH?", binary)
	}

	depth := opts.maxDepth()
	if r.MaxDepth > 0 && r.MaxDepth < depth {
		depth = r.MaxDepth
	}

	t := &Tool{Binary: binary, Framework: FrameworkRecipe}
	t.Root = buildByRecipe(binary, r, opts, run, nil, "", 0, depth, rootRaw)
	return t, nil
}

// ExtractAt reads the tree below one command path, for filling in a node that
// came back with [Node].Unread set.
//
// The returned node is that command, with its own children read the way Extract
// would have read them. It carries the path it was asked for, so a caller can
// splice it in place of the stub it replaces.
//
// Only tools read by a [Recipe] produce unread nodes today; for anything else
// this reads the subtree just the same, which is what makes it safe to call
// without first asking how a tool was read.
func ExtractAt(binary string, path []string, opts Options) (*Node, error) {
	run := limited(opts.runner(), opts.concurrency())
	r, ok := recipeFor(binary)
	if !ok {
		tool, err := Extract(binary, opts)
		if err != nil {
			return nil, err
		}
		found := findNode(tool.Root, path)
		if found == nil {
			return nil, fmt.Errorf("clisurface: %q has no command %q", binary, strings.Join(path, " "))
		}
		return found, nil
	}

	depth := opts.maxDepth()
	if r.MaxDepth > 0 && r.MaxDepth < depth {
		depth = r.MaxDepth
	}
	// LazyBelow is deliberately ignored here. This call is the moment the caller
	// asked for those children, so stopping again where the first walk stopped
	// would return the same stub and never make progress.
	r.LazyBelow = 0
	return buildByRecipe(binary, r, opts, run, path, "", len(path), depth, ""), nil
}

func findNode(n *Node, path []string) *Node {
	if n == nil {
		return nil
	}
	if len(path) == 0 {
		return n
	}
	for _, c := range n.Children {
		if c.Name == path[0] {
			return findNode(c, path[1:])
		}
	}
	return nil
}

func buildByRecipe(binary string, r Recipe, opts Options, run Runner, path []string, short string, level, maxDepth int, prefetched string) *Node {
	raw := prefetched
	if raw == "" {
		name, args := r.Help(binary, path)
		if name == "" {
			return recipeNode(binary, path, short, "", opts)
		}
		raw = run(name, args...)
	}
	n := recipeNode(binary, path, short, raw, opts)
	if level >= maxDepth {
		return n
	}

	kids := r.Children(path, stripANSI(raw))
	var real []child
	for _, k := range kids {
		if k.name == n.Name {
			continue // a row echoing its own parent is a parse artifact
		}
		real = append(real, k)
	}
	if len(real) == 0 {
		return n
	}

	n.Children = make([]*Node, len(real))
	var wg sync.WaitGroup
	for i, k := range real {
		childPath := append(append([]string{}, path...), k.name)
		if r.LazyBelow > 0 && level+1 >= r.LazyBelow {
			// Named but not read. Reading every one of aws's 438 services costs
			// twelve seconds for a screen showing one of them.
			stub := recipeNode(binary, childPath, k.desc, "", opts)
			stub.Unread = true
			n.Children[i] = stub
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			n.Children[i] = buildByRecipe(binary, r, opts, run, childPath, k.desc, level+1, maxDepth, "")
		}()
	}
	wg.Wait()
	return n
}

func recipeNode(binary string, path []string, short, raw string, opts Options) *Node {
	help := stripANSI(raw)
	n := &Node{Path: append([]string{}, path...), Short: short, Flags: uniqueFlags(help)}
	if opts.WithBody {
		n.Body = raw
	}
	if len(path) > 0 {
		n.Name = path[len(path)-1]
	} else {
		n.Name = binary
	}
	if m := usageRe.FindStringSubmatch(help); m != nil {
		n.Usage = strings.TrimSpace(m[1])
	}
	return n
}

// aws ------------------------------------------------------------------------

// awsCompleter is the program that lists aws's commands. It ships with the CLI
// and is on PATH beside it.
const awsCompleter = "aws_completer"

// awsDepth is how far aws nests: `aws <service> <operation>`, and below an
// operation the completer answers with flags rather than commands. Measured —
// `aws ec2 describe-instances ` offers --instance-ids and its siblings.
const awsDepth = 2

// awsRecipe reads aws through its own completion callback.
//
// `aws --help` is an error: it prints "the following arguments are required:
// command" and points at `aws help`, which renders a groff man page listing
// services as bullets with no descriptions. So neither of the walk's two
// assumptions holds.
//
// aws_completer is a completion callback, the same mechanism cobra's __complete
// is, and reading it runs no command. It takes COMP_LINE and COMP_POINT rather
// than argv, which is what `env` is for here: [Runner] passes no environment,
// and env(1) is on every machine this runs on, so the recipe needs no change to
// the Runner contract to reach it.
//
// The tree is 438 services over 19,292 operations, and reading it costs one
// call per service rather than one per operation, because a node at awsDepth is
// not read at all. There are no descriptions anywhere in a completion reply, so
// the tree carries names only — enough to leave with the command, which is what
// browsing is for.
var awsRecipe = Recipe{
	MaxDepth: awsDepth,
	// A service's operations are read when someone opens that service. Reading
	// all 438 up front takes twelve seconds; reading one takes 175ms.
	LazyBelow: 1,
	Help: func(_ string, path []string) (string, []string) {
		if len(path) >= awsDepth {
			return "", nil // below an operation the completer answers with flags
		}
		// The completer reads the line as a shell would present it: the words
		// typed so far and a trailing space, with the cursor at the end.
		line := "aws"
		for _, word := range path {
			line += " " + word
		}
		line += " "
		return "env", []string{
			"COMP_LINE=" + line,
			"COMP_POINT=" + strconv.Itoa(len(line)),
			awsCompleter,
		}
	},
	Children: func(_ []string, out string) []child {
		var kids []child
		seen := map[string]bool{}
		for _, line := range strings.Split(out, "\n") {
			name := strings.TrimSpace(line)
			// isCommandName refuses a leading dash, which is what keeps the
			// flags an operation completes with out of the tree.
			if !isCommandName(name) || skip[name] || seen[name] {
				continue
			}
			seen[name] = true
			kids = append(kids, child{name, ""})
		}
		return kids
	},
}

// git ------------------------------------------------------------------------

// gitRecipe reads git, which no reader reaches for two separate reasons.
//
// `git --help` lists its commands under prose headings — "start a working
// area", "work on the current change" — that no command-heading rule matches.
// `git help -a` writes "Main Porcelain Commands" over indented name-description
// rows instead, which is readable.
//
// Below the top, `git <cmd> --help` opens a man page. `git <cmd> -h` prints
// usage and flags straight to stdout with no pager, which is the whole reason
// git needs no `help` verb and no relaxed invariant.
var gitRecipe = Recipe{
	Help: func(binary string, path []string) (string, []string) {
		if len(path) == 0 {
			return binary, []string{"help", "-a"}
		}
		if gitNeverRead[path[0]] {
			return "", nil
		}
		return binary, append(append([]string{}, path...), "-h")
	},
	Children: func(path []string, help string) []child {
		if len(path) == 0 {
			return gitTopLevel(help)
		}
		return gitSynopsisChildren(path, help)
	},
}

// gitNeverRead names git subcommands whose own help is not worth what it costs.
//
// `git filter-branch -h` sleeps for ten seconds. That is deliberate on git's
// part — a warning steering people to filter-repo — and it was the whole of
// git's read time: 10024ms of a 10082ms walk, against 3-5ms for every other
// subcommand.
//
// The command still appears, with the name and description its parent's listing
// gave it. Only its own screen is skipped, so nothing is hidden.
var gitNeverRead = map[string]bool{
	"filter-branch": true,
}

// gitTopLevel reads `git help -a`, which groups its rows under twelve
// unindented headings.
//
// Every heading opens a list of commands except two — "User-facing repository,
// command and file interfaces" and "Developer-facing file formats, protocols
// and other interfaces" — which hold documentation topics: gitattributes,
// gitignore, gitglossary. Those are not runnable, and `git gitignore -h` is not
// a command.
//
// Matching on "Commands" would be the obvious test and it drops 55 real
// commands, because "Interacting with Others" and "Command aliases" do not say
// it. So the exclusion is named instead: a heading about interfaces lists
// documents. `git --list-cmds=main,others,alias` agrees on which names are
// commands, and is what to check against if git ever renames a section.
func gitTopLevel(help string) []child {
	var kids []child
	inSection := false
	for _, line := range strings.Split(help, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if !strings.HasPrefix(line, " ") {
			inSection = gitSectionListsCommands(trimmed)
			continue
		}
		if !inSection {
			continue
		}
		name, desc, found := strings.Cut(trimmed, "  ")
		if !found {
			continue
		}
		name = strings.TrimSpace(name)
		if !isCommandName(name) || skip[name] {
			continue
		}
		kids = append(kids, child{name, strings.TrimSpace(desc)})
	}
	return kids
}

// gitSectionListsCommands reports whether a heading in `git help -a` opens a
// list of commands rather than of documentation topics.
//
// The first line of that output is advice — "See 'git help <command>' to read
// about a specific subcommand" — and is unindented like a heading, so it has to
// fail this too. It does, because it is a sentence rather than a heading.
func gitSectionListsCommands(heading string) bool {
	lower := strings.ToLower(heading)
	if strings.Contains(lower, "interfaces") {
		return false
	}
	return strings.Contains(lower, "commands") ||
		strings.Contains(lower, "interacting with")
}

// gitUsageRe matches a synopsis line, whether it opens the block or continues
// it: "usage: git worktree add …" and "   or: git worktree list …".
var gitUsageRe = regexp.MustCompile(`^\s*(?:usage|or):\s+(.*)$`)

// gitSynopsisChildren reads a subcommand's own subcommands out of the usage
// block, which is the only place git names them.
//
//	usage: git worktree add [-f] [--detach] …
//	   or: git worktree list [-v | --porcelain [-z]]
//	   or: git worktree prune [-n] [-v] [--expire <expire>]
//
// There are no descriptions anywhere in that block, so the children carry none.
// A row repeating only the path, as `git remote [-v | --verbose]` does, names no
// subcommand and is skipped.
func gitSynopsisChildren(path []string, help string) []child {
	seen := map[string]bool{}
	var kids []child
	for _, line := range strings.Split(help, "\n") {
		m := gitUsageRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		name := gitNextWord(path, strings.Fields(m[1]))
		if name == "" || seen[name] || skip[name] {
			continue
		}
		seen[name] = true
		kids = append(kids, child{name, ""})
	}
	return kids
}

// gitNextWord returns the word following the command path in one synopsis line,
// and "" when the line does not continue the path with a command name.
//
// The path is matched rather than assumed at a fixed offset, because git writes
// its own global flags into the synopsis: `git remote [-v | --verbose] show
// <name>` puts the flag between the noun and the verb.
func gitNextWord(path, words []string) string {
	if len(words) == 0 || words[0] != "git" {
		return ""
	}
	rest := words[1:]
	for _, want := range path {
		for len(rest) > 0 && !isCommandName(rest[0]) {
			rest = rest[1:] // a flag or an argument spec sitting inside the path
		}
		if len(rest) == 0 || rest[0] != want {
			return ""
		}
		rest = rest[1:]
	}
	for len(rest) > 0 && !isCommandName(rest[0]) {
		rest = rest[1:]
	}
	if len(rest) == 0 {
		return ""
	}
	return rest[0]
}

// tmux -----------------------------------------------------------------------

// tmuxRecipe reads tmux, whose `--help` is 158 bytes of usage naming no
// commands at all. `tmux list-commands` names every one, with its arguments.
//
// One level only: tmux has no per-command help to walk into, and
// `list-commands` has already said everything there is.
var tmuxRecipe = Recipe{
	MaxDepth: 1,
	Help: func(binary string, path []string) (string, []string) {
		if len(path) > 0 {
			return "", nil
		}
		return binary, []string{"list-commands"}
	},
	Children: func(path []string, help string) []child {
		if len(path) > 0 {
			return nil
		}
		var kids []child
		seen := map[string]bool{}
		for _, line := range strings.Split(help, "\n") {
			fields := strings.Fields(line)
			if len(fields) == 0 {
				continue
			}
			name := fields[0]
			if !isCommandName(name) || skip[name] || seen[name] {
				continue
			}
			seen[name] = true
			// The rest of the row is the command's argument spec, which is the
			// only description tmux offers.
			kids = append(kids, child{name, strings.TrimSpace(strings.TrimPrefix(line, name))})
		}
		return kids
	},
}
