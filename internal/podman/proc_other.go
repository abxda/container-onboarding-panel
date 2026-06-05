//go:build !windows

package podman

import "os/exec"

// Hide es no-op fuera de Windows (Linux/macOS no abren ventanas de consola
// al lanzar subprocesos desde una app GUI).
func Hide(cmd *exec.Cmd) {}
