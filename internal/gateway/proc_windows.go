//go:build windows

package gateway

import (
	"os"
	"syscall"

	"golang.org/x/sys/windows"
)

// apiStopFirst reports whether the core must be asked to stop through its
// management API before any signal, because there is no portable signal that
// reaches it.
const apiStopFirst = true

// coreExeSuffix is required when resolving the bundled core next to the app.
const coreExeSuffix = ".exe"

// processGroupAttr gives the owned core its own console process group.
func processGroupAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
}

// terminateProcess is a no-op on Windows: the core was already asked to stop
// through its management API, and process signals other than Kill are
// unsupported there.
func terminateProcess(*os.Process) error {
	return nil
}

// hardKill terminates the owned core once the graceful limit expires.
func hardKill(p *os.Process) error {
	if err := p.Kill(); err != nil {
		return err
	}
	return nil
}
