//go:build !darwin && !linux

package runner

import (
	"errors"
	"io"
	"os/exec"
	"runtime"
)

// ErrPTYUnsupported is returned on platforms without a PTY implementation.
var ErrPTYUnsupported = errors.New("PTY execution is not supported on " + runtime.GOOS)

func startPTY(cmd *exec.Cmd, rows, cols uint16) (io.ReadWriteCloser, error) {
	return nil, ErrPTYUnsupported
}

func watchResize(handle io.ReadWriteCloser, done <-chan struct{}) {}

func isPTYClosedPlatformErr(err error) bool { return false }

func pumpInput(dst io.Writer, src io.Reader, done <-chan struct{}) {
	_, _ = io.Copy(dst, src)
}
