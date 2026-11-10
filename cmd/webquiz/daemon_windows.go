//go:build windows

package main

import (
	"errors"
	"os/exec"
)

// There is no daemon mode on Windows: -d prints that it is not supported.
// --stop and --status find no PID file and say that no daemon runs.
const daemonSupported = false

func detach(*exec.Cmd) {}

func processAlive(int) bool { return false }

func terminate(int) error { return errors.New("not supported on Windows") }

func kill(int) error { return errors.New("not supported on Windows") }
