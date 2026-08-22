//go:build !unix

package clisurface

import "os/exec"

// isolateGroup does nothing where process groups are not available. Canceling
// still kills the command itself, which is what the context would have done
// anyway.
func isolateGroup(*exec.Cmd) {}
