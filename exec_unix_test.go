//go:build unix

package clisurface

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A read that times out must take the whole process tree with it. Killing only
// the process that was started leaves anything it forked re-parented to init
// and still running, which is what claude-desktop does — it ignores --help and
// launches Electron.
func TestATimedOutReadLeavesNothingRunning(t *testing.T) {
	defer func(d time.Duration) { helpTimeout = d }(helpTimeout)
	helpTimeout = 300 * time.Millisecond

	pidFile := filepath.Join(t.TempDir(), "grandchild.pid")
	// Fork a grandchild that outlives the shell, record it, then block so the
	// read has to time out rather than finishing on its own.
	script := "sleep 60 & echo $! > " + pidFile + "; sleep 60"

	capture("sh", []string{"-c", script})

	raw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("the script never recorded its grandchild: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatalf("pid file held %q: %v", raw, err)
	}

	// Signal 0 tests for existence without delivering anything, and it succeeds
	// on a process that is dead but not yet reaped. Its parent was killed too,
	// so init reaps it a moment later — poll rather than read the zombie as a
	// survivor.
	deadline := time.Now().Add(3 * time.Second)
	for syscall.Kill(pid, 0) == nil {
		if time.Now().After(deadline) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
			t.Fatalf("grandchild %d survived the read; only the direct child was killed", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
