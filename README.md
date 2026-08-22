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

Two sources, in order of trust:

- **The framework's own completion callback**, where one exists. Cobra answers
  `__complete` with an exact command list. This is machine-readable and is never
  guessed at.
- **The rendered `--help` screen**, otherwise. Typer and Click draw box panels,
  clap writes indented rows under a `Commands:` heading, and a hand-rolled
  renderer writes them under an underlined one. Each shape has a reader.

Where both are available they are intersected, because a completion callback
answers with argument values as readily as with subcommand names, and only a
name the help screen also presents as a command survives.

**A bare subcommand is never run.** The shape most worth finding is a noun that
performs a read when invoked with no verb, and detecting it by running it would
fire real reads against APIs and databases.

## Consuming it

```go
import "github.com/datapointchris/clisurface"
```

Usage lands here once the API is settled rather than before, so that every
example in this file has been run as written.
