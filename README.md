# clisurface

Reads the command surface of an installed CLI: every command in its tree, with
the description, usage line and flags each one presents. It reads from the
outside. The tool being read does not cooperate and does not know it is being
read, so a CLI written in any language can be read.

A `--help` screen is written for a person. This turns it back into data.

It is a Go library and ships no binary. What happens to a tree is the
consumer's business. [helpnav](https://github.com/datapointchris/helpnav)
browses one as miller columns and hands the chosen command back to the shell.
A linter would judge one. Nothing here judges.

## Install

```bash
go get github.com/datapointchris/clisurface@latest
```

Releases are git tags. The core imports the standard library alone, so a
consumer inherits no dependencies.

## Starting from scratch

```bash
mkdir surface && cd surface
go mod init example.com/surface   # a throwaway module to hold the program below
$EDITOR main.go                   # paste the program below
go mod tidy                       # fetches the latest tagged release
go run . git                      # "git is read as recipe", then a line per command
```

```go
package main

import (
    "fmt"
    "log"
    "os"
    "strings"

    "github.com/datapointchris/clisurface"
)

func main() {
    tool, err := clisurface.Extract(os.Args[1], clisurface.Options{})
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(tool.Binary, "is read as", tool.Framework)
    tool.Walk(func(n *clisurface.Node) {
        fmt.Println(strings.Join(n.Path, " "), "—", n.Short)
    })
}
```

Any CLI on `PATH` can take the place of `git`. `Tool.Framework` names the
reader that read it. `Walk` visits every command below the root, parents
first.

`Extract` refuses a name that prints nothing for `--help`, and the program
exits 1 on it:

```text
clisurface: "nosuchtool" printed no help; is it on PATH?
```

An empty tree would be well formed and carry no sign that anything was missing.

## What a surface is, and what it is not

The **surface** is what a CLI presents: which commands exist, at what depth, and
what each says about itself. Reading one answers "what can this tool do".

The **grammar** is whether those verbs were well chosen, whether a noun should
have been namespaced, whether two tools disagree about what `list` means. That
is a judgment made *of* a surface. It belongs to whoever holds the design
standard rather than here.

This library reads surfaces. It has no opinion about them. A consumer passes in
a binary name and gets a tree back. Where a consumer keeps its tools and its
snapshots never reaches this package, and neither does what it considers good.

## `Extract` — three sources in order of trust, and never a bare subcommand

- **The framework's own completion callback**, where one exists. Cobra answers
  `__complete` with an exact command list. It is the one source here that is
  not scraped. It is probed only once the help screen names a command section,
  because an argument is not inert on a tool that takes free text.
- **The rendered `--help` screen**, otherwise. Each shape has a reader, listed
  below.
- **A hand-written recipe**, for a tool no reader can reach.

Where the first two are both available they are intersected. A completion
callback answers with argument values as readily as with subcommand names.
Only a name the help screen also presents as a command survives.

Each reader is a value of `Tool.Framework`, and `go doc` prints the set:

```bash
go doc -all github.com/datapointchris/clisurface Framework
```

- `cobra` — names from `__complete`, kept where the help screen lists them too.
- `rich` — the box-drawn panels Typer renders. A panel titled for options,
  arguments, usage or flags is skipped.
- `heading` — indented rows under a heading ending in `commands`, with or
  without a colon. clap, plain Click, oclif and yargs tools all write it.
- `section` — a bare `Commands` heading over an underline rule, with the whole
  tree on the root screen. It is assembled in one pass. A command is read on
  its own only when its row defers its verbs with `<verb>`, `<command>` or
  `<subcommand>`.
- `argparse` — Python's standard library, which lists brace-wrapped choices
  under `positional arguments:`.
- `traitlets` — the IPython framework every jupyter binary is built on, with a
  `Subcommands` heading over a rule.
- `recipe` — read through a hand-written recipe.
- `flat` — no reader found a command list.

`flat` cannot say why. A tool with no subcommands and a help shape no reader
recognizes produce the same tree.

A recipe names an **entry point**, never a command list. A list would be stale
the day the tool released. An entry point outlives it, so a subcommand the tool
adds later appears without the recipe changing. `clisurface.Recipes()` names
every tool read this way. Each is there for a reason no reader can answer:

- git lists its commands under prose headings, and `git <cmd> --help` opens a
  man page. It is read from `git help -a` and `git <cmd> -h`.
- tmux's `--help` names no commands. It is read from `tmux list-commands`.
- aws answers `--help` with an error. Its services come from `aws_completer`,
  the completion program that ships beside it.
- direnv reads `--help` as a path, so `direnv allow --help` would authorize a
  file named `--help`. Its recipe reads the root screen and stops there.

A recipe is frozen knowledge and a reader is not. A tool a reader already
handles never gets one.

**A bare subcommand is never run.** The shape most worth finding is a noun that
performs a read when invoked with no verb. Detecting it by running it would
fire real reads against APIs and databases. Discovery runs `--help` at any
depth and a framework's completion callback. The only other command it runs is
a recipe's entry point, and each of those is written by hand.

That bounds what this library asks for, not what a tool does when asked.
Answering `--help` means starting the program, so a group callback, an update
check or a lazily cloned config repo all happen before help is printed. A caller
that browses arbitrary tools should keep its own list of ones not to start.

Every invocation runs under a deadline. On Unix it runs in its own process
group, and a deadline that fires kills the whole group rather than the one
process it started. A tool that exits and leaves a daemon holding its output is
let go after a short grace period.

A non-zero exit is not a failure. Several tools print good help and exit 1, so
only the output is read. A tool with no per-command help answers
`tool sub --help` with the root screen. A command whose screen matches its
parent's is kept as a leaf rather than listing its siblings again.

## `Options` — the zero value reads for parsing, and each field moves one default

`Options{}` is usable as it stands. It runs the binary with `NO_COLOR=1`,
`TERM=dumb` and a wide `COLUMNS`. It runs up to one tool process per CPU at
once. It bounds the walk at `DefaultMaxDepth` words past the binary, and it
keeps no help bodies.

```go
clisurface.Options{
    Runner:      clisurface.DisplayRunner(90), // keep color, wrap to 90 columns
    WithBody:    true,                         // keep each node's whole help screen
    MaxDepth:    6,                            // a generated surface nests deeper
    Concurrency: 4,                            // bound the tool processes running at once
}
```

`DisplayRunner` reads a tool the way a person sees it. It declares
`TERM=xterm-256color` and forces color rather than inheriting the terminal, so
the text does not change with the terminal the consumer runs in. Those
variables reproduce what a pseudo-terminal produces, so none is involved. Two
limits are the tools' own. Cobra tools emit no color under any environment. A
tool that wraps its help in source ignores `COLUMNS`, and gh is one.

`Node.Body` is what the tool printed, color codes included. `Usage` and `Flags`
are parsed from the same screen with color codes removed. `Flags` is every
`--long` flag the screen names, and short flags are not kept. `Short` is the
description the parent's listing gave the command. The root has none. A command
its parent lists without a description has an empty one, which is every aws
name and every verb below a git subcommand.

Reading costs one process start per command, plus one completion call per
command on a cobra tool. The walk itself costs next to nothing, so
`Concurrency` is the field that moves the total. It bounds tool processes
rather than goroutines.

The depth bound is never off. A parser that mistakes an argument list for a
command list would otherwise recurse until the process died.

## `ExtractAt` — a surface too large to read whole is read where someone looks

`Node.Unread` marks a command that was named but not read. Its children are
missing rather than absent. aws comes back with every service named and none
read. Reading them all up front costs one call per service, and a person
browsing looks at a few.

```go
s3, err := clisurface.ExtractAt("aws", []string{"s3"}, clisurface.Options{})
```

`ExtractAt` reads the tree below one command path. The node it returns carries
the path it was asked for, so a caller splices it in place of the stub. It
accepts any tool, so a caller never has to ask how a tool was read first. On a
tool no recipe covers, it reads the whole tool again and returns the subtree.

## `Save`, `Load`, `Compare` — a kept surface turns a removed flag into a finding

```go
before, err := clisurface.Save(dir, []*clisurface.Tool{tool}, time.Now())
...
saved, err := clisurface.Load(dir, before.Version)
...
diff := clisurface.Compare(saved, &clisurface.Snapshot{Tools: current})
clisurface.WriteDiff(os.Stdout, diff)
```

A command or flag that was there and is not is a broken contract for whatever
called it. That is why a surface is worth keeping to subtract from.

Every function takes the directory rather than resolving one, so where
snapshots live stays the consumer's decision. `Save` writes the next version as
`<n>.json`, and `Versions` lists the ones present. A version is a plain integer
because it is what a person types when asking for the difference between two
readings.

Each side names itself in the diff header. A saved version reads as its number
and the date it was taken. A snapshot built in memory has no version and reads
`live`, so a saved reading compares against the tool as it is now. `LoadFile`
reads a snapshot kept outside the directory, in a repo or in CI, and names it by
its path.

A tool present on one side only is reported whole rather than as every command
it carries. A tool that was not installed when a snapshot was taken would
otherwise bury the one renamed flag the diff exists to surface. `Diff` carries
JSON tags for a consumer that renders its own.
