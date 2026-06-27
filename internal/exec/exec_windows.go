//go:build windows

package exec

import "os/exec"

func setSysProcAttr(_ *exec.Cmd) {}

func killProcessGroup(cmd *exec.Cmd) {
	_ = cmd.Process.Kill()
}
