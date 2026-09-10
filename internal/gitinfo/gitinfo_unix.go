//go:build unix

package gitinfo

import (
	"os/exec"
	"syscall"
)

// isolate gives git its own process group and kills the whole group on
// cancellation, so git-remote-https — git's child, which holds the pipes and
// the connection — dies with git instead of lingering until its own
// connection timeout.
func isolate(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error { return syscall.Kill(-command.Process.Pid, syscall.SIGKILL) }
}
