//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

const daemonSupported = true

// detach puts the new copy in its own session, so closing the terminal does
// not stop it (os.setsid in cli.py).
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

// processAlive is os.kill(pid, 0). EPERM means the process exists.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}

func terminate(pid int) error { return syscall.Kill(pid, syscall.SIGTERM) }

func kill(pid int) error { return syscall.Kill(pid, syscall.SIGKILL) }
