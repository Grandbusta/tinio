//go:build windows

package coding

import "os/exec"

func ownGroup(cmd *exec.Cmd) {}

// killGroup ends cmd; on Windows what it started may outlive it.
func killGroup(cmd *exec.Cmd) {
	if cmd.Process != nil {
		cmd.Process.Kill()
	}
}
