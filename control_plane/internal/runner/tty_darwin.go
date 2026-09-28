//go:build darwin

package runner

import "golang.org/x/sys/unix"

// ioctlTermiosGet/Set are TIOCGETA/TIOCSETA on BSD-derived systems (macOS),
// where TCGETS/TCSETS only exist on Linux.
const (
	ioctlTermiosGet = unix.TIOCGETA
	ioctlTermiosSet = unix.TIOCSETA
)
