package clisurface

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Framework is how a tool's tree has to be read. The distinction is not
// cosmetic: cobra ships a machine-readable command list, and everything else
// has to be scraped out of rendered help.
type Framework string

const (
	// FrameworkCobra is read from the tool's own completion callback rather
	// than from a screen, which is the only source here that is not scraped.
	FrameworkCobra Framework = "cobra"

	// FrameworkRich is typer/click: commands in box-drawn panels.
	FrameworkRich Framework = "rich"

	// FrameworkSection is a bare "Commands" heading under an underline rule,
	// with the whole tree on the root screen and no help per command, so it is
	// assembled in one pass rather than walked.
	FrameworkSection Framework = "section"

	// FrameworkHeading is a heading ending in "commands:" followed by indented
	// rows, with help at every level, so it is walked. clap writes it, and so
	// do tools with no relation to clap — the name is the shape rather than any
	// library, because npm and terraform are read by it too.
	FrameworkHeading Framework = "heading"

	// FrameworkFlat is a tool presenting no subcommands at all.
	FrameworkFlat Framework = "flat"
)

// DefaultMaxDepth bounds the walk when [Options].MaxDepth is not set.
//
// The bound exists because a parser that mistakes an argument list for a
// command list recurses until the process dies, which is exactly what a Rich
// "Arguments" panel once did. The walk is therefore always bounded by
// something, whatever the number is.
//
// Four is generous rather than tight. Measured across cobra, clap and Typer
// tools including kubectl, docker and gh, the deepest surface is three words
// past the binary, and raising the bound to eight changes no tree at all.
// Raise it through [Options] for a surface that nests further.
const DefaultMaxDepth = 4

// helpTimeout bounds one invocation. A var rather than a const so a test can
// lower it; it is unexported, so nothing outside this package can.
var helpTimeout = 20 * time.Second

// pipeDelay bounds the wait after the command itself is finished. A tool that
// daemonizes hands its stdout to a process the context cannot reach, so the
// read blocks on an EOF that never arrives. wl-copy does this and stalled a
// walk for as long as it was left running.
const pipeDelay = 2 * time.Second

// A Runner runs a command and returns everything it printed, stdout and stderr
// together. It returns the empty string when the command could not be run at
// all, which is how [Extract] tells a missing binary from a quiet one.
//
// A non-zero exit is not a failure here. Several tools print perfectly good
// help and then exit 1, so the status is deliberately ignored and only the
// output is read.
type Runner func(binary string, args ...string) string

// Options control how a surface is read. The zero value is usable: it runs the
// binary directly with color disabled, bounds the walk at [DefaultMaxDepth],
// and keeps no help bodies.
type Options struct {
	// Runner runs the tool. Nil selects the default, which executes the binary
	// with NO_COLOR, TERM=dumb and a wide COLUMNS, then strips any escape
	// sequences that survived. Supply one to read a tool under a pty, at a
	// chosen width, or with color preserved.
	Runner Runner

	// WithBody fills [Node].Body with each node's whole help screen. Off by
	// default because the bytes are real: a consumer that only wants the tree
	// should not carry every screen that produced it.
	WithBody bool

	// MaxDepth bounds how many words deep the walk goes. Zero selects
	// [DefaultMaxDepth].
	MaxDepth int

	// Concurrency bounds how many child commands are read at once. Zero selects
	// [runtime.NumCPU]; one reads serially, which is what a test wanting
	// deterministic ordering of side effects should ask for.
	//
	// This bounds processes, not goroutines. Reading a tool is spent almost
	// entirely waiting on the tool's own startup, so the limit that matters is
	// how many of those exist at once.
	Concurrency int
}

// maxDepth resolves the bound, so the zero value of Options stays meaningful.
func (o Options) maxDepth() int {
	if o.MaxDepth > 0 {
		return o.MaxDepth
	}
	return DefaultMaxDepth
}

// runner resolves the Runner, so callers never see a nil function value.
func (o Options) runner() Runner {
	if o.Runner != nil {
		return o.Runner
	}
	return execRunner
}

// concurrency resolves the process limit, so the zero value of Options stays
// meaningful.
func (o Options) concurrency() int {
	if o.Concurrency > 0 {
		return o.Concurrency
	}
	return runtime.NumCPU()
}

// limited wraps a Runner so that at most n of them are in flight.
//
// The limit is deliberately here rather than around the recursion. A parent
// waiting on its children holds no slot, so it cannot block the children it is
// waiting for — bounding the recursion instead would deadlock as soon as every
// slot was held by a parent.
func limited(run Runner, n int) Runner {
	sem := make(chan struct{}, n)
	return func(binary string, args ...string) string {
		sem <- struct{}{}
		defer func() { <-sem }()
		return run(binary, args...)
	}
}

// Node is one command in a tool's tree.
type Node struct {
	Path     []string `json:"path"`
	Name     string   `json:"name"`
	Short    string   `json:"short"`
	Usage    string   `json:"usage,omitempty"`
	Flags    []string `json:"flags,omitempty"`
	Children []*Node  `json:"children,omitempty"`

	// Body is everything the node printed for --help, filled only when
	// [Options].WithBody is set. Short, Usage and Flags are all parsed out of
	// it, so a consumer that wants to render the screen a person would see
	// takes this rather than reassembling it from the three.
	Body string `json:"body,omitempty"`

	// childIndex is scratch used while assembling a section-format tree, where
	// a parent is implied by its rows rather than listed on its own.
	childIndex map[string]*Node
}

// Leaf reports whether the node runs something rather than holding verbs.
func (n *Node) Leaf() bool { return len(n.Children) == 0 }

// Depth is how many words follow the binary name.
func (n *Node) Depth() int { return len(n.Path) }

// Tool is one binary's whole surface.
type Tool struct {
	Binary    string    `json:"binary"`
	Framework Framework `json:"framework"`
	Root      *Node     `json:"root"`
}

// Walk calls fn for every node below the root, parents before children.
func (t *Tool) Walk(fn func(*Node)) {
	if t.Root == nil {
		return
	}
	var rec func(*Node)
	rec = func(n *Node) {
		if n.Depth() > 0 {
			fn(n)
		}
		for _, c := range n.Children {
			rec(c)
		}
	}
	rec(t.Root)
}

var (
	usageRe     = regexp.MustCompile(`(?m)^\s*Usage:\s+(.*)$`)
	flagRe      = regexp.MustCompile(`(--[a-z][a-z0-9-]*)`)
	panelOpenRe = regexp.MustCompile(`^╭─+\s*(.*?)\s*─+╮`)
	panelRowRe  = regexp.MustCompile(`^[│|]\s+([a-z][a-z0-9:_-]*)\s{2,}(.*?)\s*[│|]\s*$`)
	ansiRe      = regexp.MustCompile(`\x1b\[[0-9;]*m`)
)

// skip are the commands every framework generates, which say nothing about the
// tool's own grammar.
var skip = map[string]bool{"help": true, "completion": true, "__complete": true}

// notCommandPanel are Rich panel titles that hold something other than
// subcommands. Reading an "Arguments" panel as a command list is what turned a
// 12-command tool into a 1992-node walk.
var notCommandPanel = []string{"option", "argument", "usage", "flag"}

func execRunner(binary string, args ...string) string {
	return capture(binary, args, "COLUMNS=200", "NO_COLOR=1", "TERM=dumb")
}

// DisplayRunner returns a [Runner] that reads a tool the way a person would see
// it: wrapped to width, with color left intact. Pass it to [Options] when the
// help screen is going to be rendered rather than only parsed.
//
// A terminal type is declared rather than inherited, so the text a consumer
// gets does not change with the terminal it happens to run in. TERM=dumb
// suppresses color even where FORCE_COLOR is set, which is why the default
// runner — reading a surface to parse or diff it — sets exactly that.
//
// Measured against Typer, clap and cobra tools: this reproduces everything a
// pseudo-terminal produces, so no pty is involved. Two limits are the tools'
// own. Cobra tools emit no color under any environment or terminal. And a tool
// that wraps its help in source ignores COLUMNS, gh being one.
func DisplayRunner(width int) Runner {
	return func(binary string, args ...string) string {
		return capture(binary, args, "COLUMNS="+strconv.Itoa(width), "FORCE_COLOR=1", "CLICOLOR_FORCE=1", "TERM=xterm-256color")
	}
}

func capture(binary string, args []string, env ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), helpTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Env = append(cmd.Environ(), env...)
	cmd.WaitDelay = pipeDelay
	isolateGroup(cmd)
	out, _ := cmd.CombinedOutput() // a non-zero exit still prints usable help
	return string(out)
}

// stripANSI removes escape sequences so a parser matches on the text rather
// than on whatever the tool colored it with. Every reader runs on the result;
// only [Node].Body keeps what the tool actually printed.
func stripANSI(s string) string { return ansiRe.ReplaceAllString(s, "") }

// Extract reads one binary's whole command tree.
//
// It returns an error when the binary prints nothing for --help, which is what
// a name that is not on PATH does. Reading that as a tool with no subcommands
// is the failure worth refusing: the result would be well formed, carry no
// signal that anything was missing, and confidently describe a tool that does
// not exist.
func Extract(binary string, opts Options) (*Tool, error) {
	run := limited(opts.runner(), opts.concurrency())
	rootRaw := run(binary, "--help")
	rootHelp := stripANSI(rootRaw)
	if strings.TrimSpace(rootHelp) == "" {
		return nil, fmt.Errorf("clisurface: %q printed no help; is it on PATH?", binary)
	}
	w := walk{binary: binary, fw: detectFramework(binary, rootHelp, run), opts: opts, run: run}
	t := &Tool{Binary: binary, Framework: w.fw}
	t.Root = build(w, nil, "", 0, "", rootRaw)
	return t, nil
}

// walk carries what does not change between levels, so build takes one of these
// rather than repeating four arguments down every recursive call.
type walk struct {
	binary string
	fw     Framework
	opts   Options
	run    Runner
}

// detectFramework takes the root help rather than fetching it, because Extract
// has already paid for that spawn and spawning is the whole cost of a read.
func detectFramework(binary, rootHelp string, run Runner) Framework {
	if listsCommands(rootHelp) && cobraChildren(binary, nil, run) != nil {
		return FrameworkCobra
	}
	if strings.Contains(rootHelp, "╭─") {
		return FrameworkRich
	}
	if len(sectionChildren(binary, rootHelp)) > 0 {
		return FrameworkSection
	}
	// After section, because that format writes a bare "Commands" heading with
	// an underline rule where this one writes "…commands:" and no rule.
	if len(headingChildren(rootHelp)) > 0 {
		return FrameworkHeading
	}
	return FrameworkFlat
}

// build reads one node and everything below it. prefetched is the help screen
// the caller already paid for, empty at every level but the root: Extract runs
// the root --help to detect the framework, and running it a second time here
// doubled the cost of every tool and doubled any side effect it has.
func build(w walk, path []string, short string, depth int, parentHelp, prefetched string) *Node {
	raw := prefetched
	if raw == "" {
		args := append(append([]string{}, path...), "--help")
		raw = w.run(w.binary, args...)
	}
	help := stripANSI(raw)

	n := &Node{Path: append([]string{}, path...), Short: short, Flags: uniqueFlags(help)}
	if w.opts.WithBody {
		n.Body = raw
	}
	if len(path) > 0 {
		n.Name = path[len(path)-1]
	} else {
		n.Name = w.binary
	}
	if m := usageRe.FindStringSubmatch(help); m != nil {
		n.Usage = strings.TrimSpace(m[1])
	}
	// A tool with no per-command help answers `tool sub --help` with the root
	// screen, which hand-rolled help renderers routinely do. Reading that as
	// sub's own children makes every command list its siblings, at every level,
	// until the depth bound stops it.
	if depth >= w.opts.maxDepth() || (parentHelp != "" && help == parentHelp) {
		return n
	}

	// The section format prints its whole tree on the root screen and has no
	// per-command help, so it is assembled in one pass instead of walked.
	if w.fw == FrameworkSection {
		if len(path) == 0 {
			n.Children = sectionTree(w.binary, help)
		}
		return n
	}

	var kids []child
	switch w.fw {
	case FrameworkCobra:
		// __complete is exact about names but does not say what kind of name it
		// is returning: for a leaf carrying ValidArgs it answers with the
		// argument values, so a command accepting one of three fixed keywords
		// offers those three and they parse as three subcommands. Only names
		// the help screen also presents as commands survive.
		kids = keepListed(cobraChildren(w.binary, path, w.run), helpCommandNames(help))
	case FrameworkRich:
		kids = richChildren(help)
	case FrameworkHeading:
		kids = headingChildren(help)
	case FrameworkSection, FrameworkFlat:
		kids = nil
	}
	var real []child
	for _, k := range kids {
		if k.name == n.Name {
			continue // a row echoing its own parent is a parse artifact, not a command
		}
		real = append(real, k)
	}
	if len(real) == 0 {
		return n
	}

	// Siblings are read concurrently because the cost of a read is the tool's
	// own startup, not this walk. Each goroutine writes one distinct element of
	// a pre-sized slice, which needs no lock and keeps the children in the order
	// the help screen listed them; Wait is what publishes those writes.
	n.Children = make([]*Node, len(real))
	var wg sync.WaitGroup
	for i, k := range real {
		wg.Add(1)
		go func() {
			defer wg.Done()
			childPath := append(append([]string{}, path...), k.name)
			n.Children[i] = build(w, childPath, k.desc, depth+1, help, "")
		}()
	}
	wg.Wait()
	return n
}

type child struct{ name, desc string }

// helpCommandNames is the first word of every indented row in a help screen.
//
// Deliberately not keyed off an "Available Commands:" heading. A cobra tool may
// replace the default help template with one that groups commands under its own
// titles, so the heading would have to be enumerated per tool. Flag rows start
// with a dash and drop out; example rows start with the binary name and fail the
// intersection.
func helpCommandNames(help string) map[string]bool {
	names := map[string]bool{}
	for _, line := range strings.Split(help, "\n") {
		if line == "" || !strings.HasPrefix(line, " ") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		// gh punctuates its rows as `auth:  Authenticate ...` while __complete
		// answers `auth`, so an unstripped colon empties the intersection.
		if name := strings.TrimSuffix(fields[0], ":"); isCommandName(name) {
			names[name] = true
		}
	}
	return names
}

func keepListed(kids []child, listed map[string]bool) []child {
	out := make([]child, 0, len(kids))
	for _, k := range kids {
		if listed[k.name] {
			out = append(out, k)
		}
	}
	return out
}

// cobraChildren asks cobra's own completion callback. Returns nil when the tool
// is not cobra, which is also how the framework is detected.
//
// The root call is gated on [listsCommands]. Detecting by probe means running
// every tool with an argument it may not implement, and a tool that takes free
// text acts on that argument instead of rejecting it.
func cobraChildren(binary string, path []string, run Runner) []child {
	args := append([]string{"__complete"}, path...)
	args = append(args, "")
	out := stripANSI(run(binary, args...))
	if !strings.Contains(out, ":") || !strings.Contains(out, "Completion ended") {
		return nil
	}
	var kids []child
	for _, line := range strings.Split(out, "\n") {
		if line == "" || strings.HasPrefix(line, ":") || strings.HasPrefix(line, "Completion") {
			continue
		}
		name, desc, _ := strings.Cut(line, "\t")
		name = strings.TrimSpace(name)
		if name == "" || strings.HasPrefix(name, "-") || skip[name] || !isCommandName(name) {
			continue
		}
		kids = append(kids, child{name, strings.TrimSpace(desc)})
	}
	if kids == nil {
		return []child{} // cobra answered, this node simply has no subcommands
	}
	return kids
}

func richChildren(help string) []child {
	var kids []child
	inPanel := false
	for _, line := range strings.Split(help, "\n") {
		if m := panelOpenRe.FindStringSubmatch(line); m != nil {
			inPanel = !titleIsNotCommands(m[1])
			continue
		}
		if strings.HasPrefix(line, "╰") {
			inPanel = false
			continue
		}
		if !inPanel {
			continue
		}
		if m := panelRowRe.FindStringSubmatch(line); len(m) == 3 && !skip[m[1]] {
			kids = append(kids, child{m[1], strings.TrimSpace(m[2])})
		}
	}
	return kids
}

// headingChildren reads a heading ending in "commands:", then the indented rows
// under it, until an unindented line ends the block.
//
// Unlike the section format this is walked rather than assembled in one pass,
// because a tool writing this shape gives every group its own help screen
// carrying its own heading.
//
// Nothing about the rows is pinned, because the tools writing this shape
// disagree about all of it. The indent is two spaces in some and four in
// others. The block is not always first — it can sit below "Options:", and a
// tool may open several under headings of its own. A row may carry aliases
// after the name, as `build, b`, of which only the first is a command. And a
// row may carry no description at all, listing several commands separated by
// commas instead.
// commaRun reads a row that lists several commands separated by commas, which
// is how a tool with too many to describe individually prints them.
//
// All or nothing per row, and every token has to be a command name. That is
// what keeps the wrapped continuation of a long description out: prose wraps
// without commas, and the moment one token is not a name the row is refused
// whole rather than contributing the tokens that happened to look right.
//
// Descriptions are genuinely absent here rather than dropped. A tool printing
// its commands this way has none to give at this level; each one still
// describes itself in its own help.
func commaRun(row string) []child {
	parts := strings.Split(row, ",")
	if len(parts) < 2 {
		return nil
	}
	out := make([]child, 0, len(parts))
	for _, part := range parts {
		name := strings.TrimSpace(part)
		if name == "" {
			continue // the trailing comma before the list wraps
		}
		if !isCommandName(name) {
			return nil
		}
		if skip[name] {
			continue
		}
		out = append(out, child{name: name})
	}
	return out
}

// listsCommands reports whether a help screen names a command section at all.
//
// It gates the __complete probe, and an argument is the reason it has to. A
// token handed to a tool that does not implement it is not inert: wl-copy takes
// free text, so probing it wrote "__complete" to the clipboard and left a
// daemon holding the pipe. A tool that lists no commands has nothing the probe
// could tell us, and is exactly the tool the probe can damage.
//
// Deliberately broader than [isCommandHeading] in both directions it can be.
// The colon is optional, because gh writes "CORE COMMANDS" and is still cobra.
// Indentation is allowed, because sesh centers its "COMMANDS" heading inside a
// styled box and is still cobra.
//
// What replaces the indent test is the gutter: a heading is the whole line,
// while a row carries a description after two or more spaces. That is what
// keeps terraform's "metadata  Metadata related commands:" out.
func listsCommands(help string) bool {
	for _, line := range strings.Split(help, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.Contains(trimmed, "  ") || strings.Contains(trimmed, "\t") {
			continue
		}
		lower := strings.ToLower(strings.TrimSuffix(trimmed, ":"))
		if strings.HasSuffix(lower, "commands") {
			return true
		}
	}
	return false
}

// isCommandHeading reports whether a line opens a block of commands.
//
// The suffix is matched rather than the whole line, because what precedes it
// varies: "Commands:", "SUBCOMMANDS:", and a tool that splits its list into
// "Main commands:" and "All other commands:". A tool may open several such
// blocks and every one of them is read.
//
// The heading must be unindented. Without that, a command row whose description
// happens to end in "commands:" would open a block of its own.
func isCommandHeading(line, trimmed string) bool {
	if line == "" || strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
		return false
	}
	return strings.HasSuffix(strings.ToLower(trimmed), "commands:")
}

func headingChildren(help string) []child {
	var kids []child
	inSection := false
	for _, line := range strings.Split(help, "\n") {
		trimmed := strings.TrimSpace(line)
		if isCommandHeading(line, trimmed) {
			inSection = true
			continue
		}
		if !inSection {
			continue
		}
		if trimmed == "" {
			continue
		}
		if !strings.HasPrefix(line, " ") {
			// Only a heading ends the block, and the colon is what marks one.
			// zk groups its commands under bare unindented labels — NOTEBOOK,
			// NOTES — and closing on any unindented line dropped every command
			// underneath them. "Flags:" still closes it.
			if strings.HasSuffix(trimmed, ":") {
				inSection = false
			}
			continue
		}
		// The two-space gutter is required rather than optional: it is what
		// separates a command row from the wrapped continuation of a long
		// description, which is indented and would otherwise read as a command
		// named after its own last word.
		name, desc, found := strings.Cut(trimmed, "  ")
		if !found {
			// No gutter, so there is no description to the right. Either the
			// row is a comma-separated run of names, or it is the wrapped
			// continuation of the description above it.
			kids = append(kids, commaRun(trimmed)...)
			continue
		}
		name, _, _ = strings.Cut(name, ",")
		name = strings.TrimSpace(name)
		if !isCommandName(name) || skip[name] {
			continue
		}
		kids = append(kids, child{name, strings.TrimSpace(desc)})
	}
	return kids
}

// sectionTree assembles the whole tree from the root screen's rows. A row may
// name more than one word, as when a screen lists `sync setup`, `sync status`
// and `sync push` rather than a `sync` parent — so the leading command words
// become a path and the shared prefix becomes a parent node that the help never
// lists on its own.
func sectionTree(binary, help string) []*Node {
	var roots []*Node
	byName := map[string]*Node{}

	for _, c := range sectionChildren(binary, help) {
		words := strings.Fields(c.name)
		parent := &roots
		var path []string
		lookup := byName
		for i, w := range words {
			path = append(path, w)
			node, ok := lookup[w]
			if !ok {
				node = &Node{Path: append([]string{}, path...), Name: w}
				lookup[w] = node
				*parent = append(*parent, node)
			}
			if i == len(words)-1 {
				// First row wins. A screen may list `install` and
				// `install --check` as separate rows; both reduce to the same
				// command, and the second would otherwise overwrite the real
				// description with the flag's.
				if node.Short == "" {
					node.Short = c.desc
				}
			} else {
				if node.childIndex == nil {
					node.childIndex = map[string]*Node{}
				}
				lookup = node.childIndex
				parent = &node.Children
			}
		}
	}
	return roots
}

// sectionChildren reads the hand-rolled shape: a "Commands" heading, an
// underline rule, then indented rows. Some such screens prefix each row with
// the tool's own name and some do not, so the binary name is stripped when
// present.
func sectionChildren(binary, help string) []child {
	var kids []child
	inSection := false
	for _, line := range strings.Split(help, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "Commands" {
			inSection = true
			continue
		}
		if inSection && trimmed != "" && isRule(trimmed) {
			continue
		}
		if inSection && trimmed == "" {
			continue
		}
		if inSection && !strings.HasPrefix(line, " ") {
			inSection = false // a new unindented heading ends the block
			continue
		}
		if !inSection {
			continue
		}
		// Split on the two-space gutter rather than pattern-matching the row:
		// the left side is "name [args]" in every variant of this format, and
		// the arg spellings vary too much to enumerate ([--tag <name>],
		// <verb>, --to <path>).
		row := strings.TrimPrefix(strings.TrimLeft(line, " "), binary+" ")
		left, desc, found := strings.Cut(row, "  ")
		if !found {
			continue
		}
		// Keep every leading command word and stop at the first argument
		// placeholder, so `sync push` stays two words while
		// `backup [--tag <name>]` stays one.
		var words []string
		for _, w := range strings.Fields(left) {
			if !isCommandName(w) {
				break
			}
			words = append(words, w)
		}
		if len(words) == 0 || skip[words[0]] {
			continue
		}
		kids = append(kids, child{strings.Join(words, " "), strings.TrimSpace(desc)})
	}
	return kids
}

func isRule(s string) bool {
	for _, r := range s {
		if r != '─' && r != '-' && r != '━' && r != '=' {
			return false
		}
	}
	return len(s) > 0
}

func titleIsNotCommands(title string) bool {
	t := strings.ToLower(title)
	for _, w := range notCommandPanel {
		if strings.Contains(t, w) {
			return true
		}
	}
	return false
}

func isCommandName(s string) bool {
	if s == "" || s[0] < 'a' || s[0] > 'z' {
		return false // a leading dash is a flag, not a command
	}
	for _, r := range s {
		lower := r >= 'a' && r <= 'z'
		digit := r >= '0' && r <= '9'
		punct := r == '-' || r == '_' || r == ':'
		if !lower && !digit && !punct {
			return false
		}
	}
	return true
}

func uniqueFlags(help string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range flagRe.FindAllStringSubmatch(help, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			out = append(out, m[1])
		}
	}
	return out
}
