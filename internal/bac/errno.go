// SPDX-License-Identifier: MIT

package bac

import (
	"errors"
	"syscall"
)

var errAddrInUse error = syscall.EADDRINUSE

// isErrno reports whether err is the operating system error number n.
func isErrno(err error, n uintptr) bool {
	var errno syscall.Errno
	return errors.As(err, &errno) && uintptr(errno) == n
}
