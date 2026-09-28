// Package runner executes a resolved operator entry under a PTY.
//
// A PTY is required (not just nice to have): `fixer`, `ai-pro` and `ssh-tui`
// are curses TUIs and refuse to render without a terminal. The menu keeps its
// own screen until an entry is selected, hands the real terminal to the child
// for the duration of the run, streams output live, and takes the screen back
// when the child exits.
package runner

import (
	"bytes"
	"errors"
	"io"
	"os/exec"
	"sync"
	"sync/atomic"

	"github.com/fixer-mcp/control-plane/internal/ansi"
	"github.com/fixer-mcp/control-plane/internal/registry"
	"github.com/fixer-mcp/control-plane/internal/vpnenv"
)

// DefaultCaptureBytes is how much recent output is kept for the menu summary.
const DefaultCaptureBytes = 8 * 1024

// EnvFor returns the child environment with the operator's VPN marker values
// sourced on top of base. The marker wins, matching the launcher canon.
func EnvFor(base []string, marker vpnenv.Status) []string {
	return vpnenv.Env(base, marker.Values)
}

// Options configures a single child run.
type Options struct {
	// Env is the complete child environment. Use EnvFor to build it.
	Env []string
	// Dir is the child working directory. Empty means inherit.
	Dir string
	// Rows and Cols size the PTY. Zero values fall back to the caller TTY.
	Rows, Cols uint16
	// CaptureBytes bounds the retained tail. <=0 uses DefaultCaptureBytes.
	CaptureBytes int
}

// focusState tracks whether the child asked the real terminal to report focus.
// While it did not, tmux focus reports are pure noise for us: with the operator
// tty in canonical mode they were echoed as `^[[O^[[I` right under the child's
// prompt, and the child never asked for them.
type focusState struct {
	enabled atomic.Bool
}

func (f *focusState) observe(chunk []byte) {
	if bytes.Contains(chunk, []byte("\x1b[?1004h")) {
		f.enabled.Store(true)
	}
	if bytes.Contains(chunk, []byte("\x1b[?1004l")) {
		f.enabled.Store(false)
	}
}

func (f *focusState) filter(data []byte) []byte {
	if f.enabled.Load() {
		return data
	}
	return stripFocusEvents(data)
}

// stripFocusEvents removes CSI I / CSI O (focus in / focus out) sequences.
func stripFocusEvents(data []byte) []byte {
	if !bytes.Contains(data, []byte("\x1b[")) {
		return data
	}
	out := make([]byte, 0, len(data))
	for i := 0; i < len(data); i++ {
		if data[i] == 0x1b && i+2 < len(data) && data[i+1] == '[' && (data[i+2] == 'I' || data[i+2] == 'O') {
			i += 2
			continue
		}
		out = append(out, data[i])
	}
	return out
}

// observedWriter mirrors the child's terminal requests while forwarding its
// output to the operator terminal and the capture ring.
func observedWriter(out io.Writer, ring *Ring, focus *focusState) io.Writer {
	writers := make([]io.Writer, 0, 2)
	if out != nil {
		writers = append(writers, out)
	}
	if ring != nil {
		writers = append(writers, ring)
	}
	if len(writers) == 0 {
		return io.Discard
	}
	return &focusObserveWriter{inner: io.MultiWriter(writers...), state: focus}
}

type focusObserveWriter struct {
	inner io.Writer
	state *focusState
}

func (w *focusObserveWriter) Write(p []byte) (int, error) {
	w.state.observe(p)
	return w.inner.Write(p)
}

// ExecCommand is a tea.ExecCommand implementation that runs the entry under a
// PTY, streams output to the terminal Bubble Tea hands over, and keeps a
// bounded tail for the post-run menu summary.
type ExecCommand struct {
	entry   registry.Resolved
	opts    Options
	capture int

	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer

	ring *Ring
	err  error

	// focus mirrors the child's focus-reporting request so we only forward the
	// terminal's focus events when the child actually asked for them.
	focus focusState
}

// NewExecCommand builds a PTY-backed command for the resolved entry.
func NewExecCommand(entry registry.Resolved, opts Options) *ExecCommand {
	capture := opts.CaptureBytes
	if capture <= 0 {
		capture = DefaultCaptureBytes
	}
	return &ExecCommand{
		entry:   entry,
		opts:    opts,
		capture: capture,
	}
}

// SetStdin implements tea.ExecCommand.
func (c *ExecCommand) SetStdin(r io.Reader) { c.stdin = r }

// SetStdout implements tea.ExecCommand.
func (c *ExecCommand) SetStdout(w io.Writer) { c.stdout = w }

// SetStderr implements tea.ExecCommand.
func (c *ExecCommand) SetStderr(w io.Writer) { c.stderr = w }

// Capture returns the sanitized tail of the child output.
func (c *ExecCommand) Capture() string {
	if c.ring == nil {
		return ""
	}
	return ansi.Strip(c.ring.String())
}

// Err returns the run failure, if any.
func (c *ExecCommand) Err() error { return c.err }

// Run starts the child under a PTY and blocks until it exits.
//
// The PTY merges stdout and stderr, so both are streamed through the same copy
// loop. Output goes to the terminal writer and, in parallel, to a bounded ring
// buffer used by Capture.
func (c *ExecCommand) Run() error {
	if c.entry.Binary == "" {
		c.err = errors.New("entry " + c.entry.ID + " has no resolved binary")
		return c.err
	}

	cmd := exec.Command(c.entry.Binary, c.entry.Args...)
	cmd.Env = c.opts.Env
	if c.opts.Dir != "" {
		cmd.Dir = c.opts.Dir
	}

	handle, err := startPTY(cmd, c.opts.Rows, c.opts.Cols)
	if err != nil {
		c.err = err
		return err
	}

	done := make(chan struct{})
	watchResize(handle, done)

	// The child owns a PTY of its own, so the operator's real tty is ours to
	// manage while it runs. Leaving it canonical (what Bubble Tea's terminal
	// release does) meant tmux focus reports were echoed under the child's
	// prompt, and Ctrl+C was swallowed by our own signal handler instead of
	// reaching the child. Raw mode hands the bytes through untouched.
	if restoreInput, rawErr := makeRawInput(); rawErr == nil {
		defer restoreInput()
	}

	// The input pump must stop before Run returns: Bubble Tea restarts its own
	// input reader right after the child exits, and a pump still blocked in a
	// read would swallow the next keystroke. pumpInput guarantees that by
	// polling with a short timeout and watching done.
	var pump sync.WaitGroup
	if c.stdin != nil {
		pump.Add(1)
		go func() {
			defer pump.Done()
			pumpInput(handle, c.stdin, done, c.focus.filter)
		}()
	}

	c.ring = NewRing(c.capture)

	out := c.stdout
	if out == nil {
		out = c.stderr
	}

	_, copyErr := io.Copy(observedWriter(out, c.ring, &c.focus), handle)
	waitErr := cmd.Wait()

	// The child is gone. Close the PTY and stop forwarding input before Run
	// returns: Bubble Tea restarts its own input reader immediately afterwards.
	_ = handle.Close()
	close(done)
	pump.Wait()

	if copyErr != nil && !isPTYClosedErr(copyErr) && waitErr == nil {
		c.err = copyErr
		return copyErr
	}
	if waitErr != nil {
		c.err = waitErr
		return waitErr
	}
	return nil
}

// isPTYClosedErr reports whether the read error is the normal "child exited"
// signal from a PTY master.
func isPTYClosedErr(err error) bool {
	if err == nil || errors.Is(err, io.EOF) {
		return true
	}
	return isPTYClosedPlatformErr(err)
}

// Ring is a bounded byte buffer that keeps the most recent bytes written.
type Ring struct {
	mu   sync.Mutex
	data []byte
	max  int
}

// NewRing builds a ring of at most max bytes.
func NewRing(max int) *Ring {
	if max <= 0 {
		max = DefaultCaptureBytes
	}
	return &Ring{max: max}
}

// Write appends p, discarding the oldest bytes beyond the limit.
func (r *Ring) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.data = append(r.data, p...)
	if len(r.data) > r.max {
		drop := len(r.data) - r.max
		r.data = append(r.data[:0], r.data[drop:]...)
	}
	return len(p), nil
}

// String returns the retained bytes as a string.
func (r *Ring) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return string(r.data)
}
