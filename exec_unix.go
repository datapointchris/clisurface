//go:build unix

package clisurface

import (
	"os/exec"
	"syscall"
)

// isolateGroup puts the command in its own process group and cancels by killing
// that group rather than the one process.
//
// A tool that forks leaves children the context cannot reach. Canceling kills
// the process it started, and everything that process spawned re-parents to
// init and carries on. Measured 2026-08-21: claude-desktop ignores --help and
// launches Electron, so reading it left three zygotes and a crash handler
// running after the walk had moved on.
//
// Detaching from the caller's process group is wanted on its own. It stops a
// tool that reaches for the terminal taking the one the reader is drawing to.
func isolateGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
