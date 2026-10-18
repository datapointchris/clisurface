// Package clisurface reads the command surface of an installed CLI: every
// command in its tree, with the description, usage line and flags each one
// presents.
//
// Everything works from the outside. It runs --help, plus the machine-readable
// completion callback a framework ships where there is one, and infers the rest
// from the rendered screen. The CLI does not cooperate and does not know it is
// being read, so a tool written in any language can be read by this one.
//
// Nothing the walk discovers is run bare. The shape most worth finding is a
// noun that performs a read when invoked with no verb, and detecting it by
// running it would fire real reads against APIs and databases. Three kinds of
// command are run: --help at any depth, a framework's completion callback, and
// the entry point a hand-written recipe names for a tool no reader can reach,
// such as `git help -a` or `tmux list-commands`.
package clisurface
