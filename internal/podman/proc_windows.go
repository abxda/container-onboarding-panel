//go:build windows

package podman

import (
	"os/exec"
	"syscall"
)

// Hide evita que los subprocesos de consola (podman.exe, winget, …) abran una
// ventana de consola visible cuando los lanza una app GUI sin consola (Wails).
// Sin esto, el sondeo periódico del panel parpadea abriendo y cerrando consolas
// y le roba el foco al usuario. CREATE_NO_WINDOW (0x08000000) lo evita.
func Hide(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}
