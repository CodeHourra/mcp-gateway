//go:build !windows

package gateway

import (
	"errors"
	"os"
	"syscall"
)

// tryLockFile takes an exclusive non-blocking lock. busy reports that another
// desktop or connect process holds it.
func tryLockFile(f *os.File) (busy bool, err error) {
	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err == nil {
		return false, nil
	}
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return true, nil
	}
	return false, err
}
