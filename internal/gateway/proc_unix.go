//go:build !windows

package gateway

import (
	"errors"
	"os"
	"syscall"
)

// apiStopFirst reports whether the core must be asked to stop through its
// management API before any signal, because there is no portable signal that
// reaches it.
const apiStopFirst = false

// coreExeSuffix is empty on Unix-like systems.
const coreExeSuffix = ""

// processGroupAttr places the owned core in its own process group so that
// cleanup never reaches an unrelated process.
func processGroupAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}

// detachedAttr lets the update helper outlive this process.
func detachedAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}

// terminateProcess requests the core's normal exit so it can clean up its
// upstreams. Only the exact process group created here is signalled.
func terminateProcess(p *os.Process) error {
	return p.Signal(syscall.SIGTERM)
}

// hardKill terminates the owned process group once the graceful limit expires.
func hardKill(p *os.Process) error {
	err := syscall.Kill(-p.Pid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return os.ErrProcessDone
	}
	return err
}
