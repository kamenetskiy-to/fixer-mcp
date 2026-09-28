//go:build linux

package runner

import "golang.org/x/sys/unix"

// ioctlTermiosGet/Set are TCGETS/TCSETS on Linux; BSD-derived systems use
// TIOCGETA/TIOCSETA instead.
const (
	ioctlTermiosGet = unix.TCGETS
	ioctlTermiosSet = unix.TCSETS
)
