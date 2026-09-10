//go:build windows

package gitinfo

import "os/exec"

// isolate is a no-op on Windows, where a cancelled command is killed alone
// and WaitDelay releases the pipes its child keeps open.
func isolate(*exec.Cmd) {}
