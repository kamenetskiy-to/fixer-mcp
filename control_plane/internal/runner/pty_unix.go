//go:build darwin || linux

package runner

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"
)

// startPTY starts cmd attached to a new PTY pair.
func startPTY(cmd *exec.Cmd, rows, cols uint16) (io.ReadWriteCloser, error) {
	if rows > 0 && cols > 0 {
		return pty.StartWithSize(cmd, &pty.Winsize{Rows: rows, Cols: cols})
	}
	f, err := pty.Start(cmd)
	if err != nil {
		return nil, err
	}
	// Best effort: when the caller has a real TTY, inherit its size. A
	// headless run (tests, automation) simply keeps the default size.
	_ = pty.InheritSize(os.Stdin, f)
	return f, nil
}

// watchResize propagates terminal resize events to the child PTY until done is
// closed.
func watchResize(handle io.ReadWriteCloser, done <-chan struct{}) {
	f, ok := handle.(*os.File)
	if !ok {
		return
	}
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGWINCH)
	go func() {
		defer signal.Stop(ch)
		for {
			select {
			case <-done:
				return
			case <-ch:
				_ = pty.InheritSize(os.Stdin, f)
			}
		}
	}()
}

// isPTYClosedPlatformErr reports the platform-specific "PTY closed" error.
func isPTYClosedPlatformErr(err error) bool {
	return errors.Is(err, syscall.EIO)
}

// pumpInput forwards terminal input into the PTY until done is closed or the
// PTY write side disappears.
//
// The copy must stop immediately after the child exits: Bubble Tea restarts
// its own input reader as soon as Run returns, and a leftover blocked read
// would steal the next keystroke from the menu. A plain io.Copy cannot be
// interrupted, so a real file is polled with a short timeout instead.
func pumpInput(dst io.Writer, src io.Reader, done <-chan struct{}) {
	file, ok := src.(*os.File)
	if !ok {
		// Non-file readers terminate on their own (tests, closed pipes).
		_, _ = io.Copy(dst, src)
		return
	}
	buf := make([]byte, 4096)
	fds := []unix.PollFd{{Fd: int32(file.Fd()), Events: unix.POLLIN}}
	for {
		select {
		case <-done:
			return
		default:
		}
		fds[0].Revents = 0
		n, err := unix.Poll(fds, pollIntervalMillis)
		if err != nil {
			if err == unix.EINTR {
				continue
			}
			return
		}
		if n == 0 {
			continue
		}
		select {
		case <-done:
			return
		default:
		}
		read, readErr := file.Read(buf)
		if read > 0 {
			if _, writeErr := dst.Write(buf[:read]); writeErr != nil {
				return
			}
		}
		if readErr != nil {
			return
		}
	}
}

// pollIntervalMillis bounds how long pumpInput can keep reading after the
// child exits.
const pollIntervalMillis = 100
