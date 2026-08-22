# clisurface

Read the command surface of an installed CLI — every command in its tree, with
the description, usage line and flags each one presents — from the outside, with
no cooperation from the tool being read.

A `--help` screen is written for a person. This turns it back into data.

## What a surface is, and what it is not

The **surface** is what a CLI presents: which commands exist, at what depth, and
what each says about itself. Reading one answers "what can this tool do".

The **grammar** is whether those verbs were well chosen, whether a noun should
have been namespaced, whether two tools disagree about what `list` means. That
is a judgment made *of* a surface, and it belongs to whoever holds the design
standard rather than here.

This library reads surfaces. It has no opinion about them.

## How it reads

Three sources, in order of trust:

- **The framework's own completion callback**, where one exists. Cobra answers
  `__complete` with an exact command list. This is machine-readable and is never
  guessed at. It is probed only once the help screen names a command section,
  because an argument is not inert on a tool that takes free text.
- **The rendered `--help` screen**, otherwise. Typer and Click draw box panels,
  clap writes indented rows under a `Commands:` heading, Python's argparse lists
  brace-wrapped choices under `positional arguments:`, and a hand-rolled
  renderer writes them under an underlined one. Each shape has a reader.
- **A hand-written recipe**, for a tool no reader can reach. `git --help` lists
  its commands under prose headings and `git <cmd> --help` opens a man page, so
  git is read from `git help -a` and `git <cmd> -h` instead. `tmux --help` names
  no commands at all, so tmux is read from `tmux list-commands`.

Where both of the first two are available they are intersected, because a
completion callback answers with argument values as readily as with subcommand
names, and only a name the help screen also presents as a command survives.

A recipe names an **entry point**, never a command list. A list would be stale
the day the tool released; an entry point outlives it, so a new subcommand
appears without the table changing.

**A bare subcommand is never run.** The shape most worth finding is a noun that
performs a read when invoked with no verb, and detecting it by running it would
fire real reads against APIs and databases.

That bounds what this library asks for, not what a tool does when asked.
Answering `--help` means starting the program, so a group callback, an update
check or a lazily cloned config repo all happen before help is printed. A caller
that browses arbitrary tools should keep its own list of ones not to start.

## Reading a tool

```go
tool, err := clisurface.Extract("uv", clisurface.Options{})
if err != nil {
    return err
}
tool.Walk(func(n *clisurface.Node) {
    fmt.Println(strings.Join(n.Path, " "), "—", n.Short)
})
```

`Options{}` is usable as it stands. It runs the binary with color disabled and
a wide terminal, reads sibling commands concurrently across the machine's CPUs,
bounds the walk at four words deep, and keeps no help bodies.

Each field changes one of those:

```go
clisurface.Options{
    Runner:      clisurface.DisplayRunner(90), // keep color, wrap to 90 columns
    WithBody:    true,                         // keep each node's whole help screen
    MaxDepth:    6,                            // a generated surface nests deeper
    Concurrency: 4,                            // bound the tools running at once
}
```

`Node.Body` is what the tool printed, escapes included. `Short`, `Usage` and
`Flags` are parsed out of the same text with the escapes removed, so a reader
never has to care which runner produced it.

Reading is bounded by the *tool's* startup, not by this walk. A cobra tool
answers `--help` in about 4ms and a Python one in about 200ms, so concurrency is
the only thing that moves the total: `gh` at 229 nodes takes 2.5s rather than
13s.

## Keeping a surface to subtract from later

```go
before, err := clisurface.Save(dir, []*clisurface.Tool{tool}, time.Now())
...
loaded, err := clisurface.Load(dir, before.Version)
...
diff := clisurface.Compare(loaded, &clisurface.Snapshot{Tools: current})
clisurface.WriteDiff(os.Stdout, diff)
```

A command or flag that was there and is not is a broken contract for whatever
called it, which is why a surface is worth keeping around to subtract from.

Every function takes the directory rather than resolving one, so where snapshots
live stays the consumer's decision. A snapshot with no version labels itself
`live`, so a saved reading can be compared against the tool as it is now.
