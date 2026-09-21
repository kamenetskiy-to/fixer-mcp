package runner

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/fixer-mcp/control-plane/internal/registry"
	"github.com/fixer-mcp/control-plane/internal/vpnenv"
)

func TestRingKeepsTheMostRecentBytes(t *testing.T) {
	r := NewRing(5)
	_, _ = r.Write([]byte("abc"))
	_, _ = r.Write([]byte("defg"))
	if got := r.String(); got != "cdefg" {
		t.Fatalf("Ring.String() = %q, want cdefg", got)
	}
}

func TestRingWriteReportsFullLength(t *testing.T) {
	r := NewRing(4)
	n, err := r.Write([]byte("0123456789"))
	if err != nil || n != 10 {
		t.Fatalf("Write() = (%d, %v), want (10, nil)", n, err)
	}
}

func TestEnvForSourcesVPNMarker(t *testing.T) {
	marker := vpnenv.Status{
		Active: true,
		Values: map[string]string{"HTTPS_PROXY": "http://127.0.0.1:1055"},
	}
	got := EnvFor([]string{"PATH=/usr/bin", "HTTPS_PROXY=http://stale"}, marker)
	joined := strings.Join(got, "\n")
	if !strings.Contains(joined, "HTTPS_PROXY=http://127.0.0.1:1055") {
		t.Fatalf("marker value missing:\n%s", joined)
	}
	if strings.Contains(joined, "http://stale") {
		t.Fatalf("stale proxy survived:\n%s", joined)
	}
}

func TestExecCommandWithoutBinaryFails(t *testing.T) {
	cmd := NewExecCommand(registry.Resolved{Entry: registry.Entry{ID: "ghost"}}, Options{})
	if err := cmd.Run(); err == nil {
		t.Fatal("expected an error for an entry with no resolved binary")
	}
}

func TestExecCommandStreamsPTYOutput(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("PTY runner only supports darwin/linux")
	}
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("sh is not available")
	}
	entry := registry.Resolved{
		Entry:  registry.Entry{ID: "echo", Command: "sh", Args: []string{"-c", "printf 'hello-pty\\n'"}},
		Binary: sh,
	}
	command := NewExecCommand(entry, Options{Env: os.Environ(), Rows: 24, Cols: 80})
	var out bytes.Buffer
	command.SetStdin(strings.NewReader(""))
	command.SetStdout(&out)
	command.SetStderr(io.Discard)

	if err := command.Run(); err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if !strings.Contains(out.String(), "hello-pty") {
		t.Fatalf("stdout did not receive PTY output: %q", out.String())
	}
	if !strings.Contains(command.Capture(), "hello-pty") {
		t.Fatalf("capture did not retain PTY output: %q", command.Capture())
	}
}

func TestInputPumpStopsAfterChildExit(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("PTY runner only supports darwin/linux")
	}
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("sh is not available")
	}

	// Regression: a pump goroutine left blocked in a read after the child exits
	// swallows the next keystroke from the menu. Run a fast child, then write a
	// byte into the stdin pipe and require that the byte is still there.
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()

	entry := registry.Resolved{
		Entry:  registry.Entry{ID: "quick", Command: "sh", Args: []string{"-c", "exit 0"}},
		Binary: sh,
	}
	command := NewExecCommand(entry, Options{Env: os.Environ(), Rows: 24, Cols: 80})
	command.SetStdin(reader)
	command.SetStdout(io.Discard)
	command.SetStderr(io.Discard)
	if err := command.Run(); err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	time.Sleep(200 * time.Millisecond)
	if _, err := writer.Write([]byte("keystroke")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := reader.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	buf := make([]byte, 16)
	n, err := reader.Read(buf)
	if err != nil {
		t.Fatalf("input was consumed by a leaked pump: %v", err)
	}
	if got := string(buf[:n]); got != "keystroke" {
		t.Fatalf("read %q, want keystroke", got)
	}
}

func TestExecCommandPropagatesExitCode(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("PTY runner only supports darwin/linux")
	}
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("sh is not available")
	}
	entry := registry.Resolved{
		Entry:  registry.Entry{ID: "fail", Command: "sh", Args: []string{"-c", "exit 3"}},
		Binary: sh,
	}
	command := NewExecCommand(entry, Options{Env: os.Environ(), Rows: 24, Cols: 80})
	command.SetStdin(strings.NewReader(""))
	command.SetStdout(io.Discard)
	command.SetStderr(io.Discard)

	runErr := command.Run()
	if runErr == nil {
		t.Fatal("expected a non-zero exit to surface as an error")
	}
	var exitErr *exec.ExitError
	if !errors.As(runErr, &exitErr) {
		t.Fatalf("error = %v, want *exec.ExitError", runErr)
	}
	if exitErr.ExitCode() != 3 {
		t.Fatalf("exit code = %d, want 3", exitErr.ExitCode())
	}
	if !errors.Is(command.Err(), runErr) {
		t.Fatalf("Err() = %v, want %v", command.Err(), runErr)
	}
}
