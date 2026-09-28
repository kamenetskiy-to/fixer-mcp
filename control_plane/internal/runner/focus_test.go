package runner

import (
	"strings"
	"testing"
)

// tmux reports focus changes to the pane. While the child had not asked for
// them, those bytes were echoed under its prompt as `^[[O^[[I` garbage.
func TestStripFocusEvents(t *testing.T) {
	input := []byte("Press enter to continue\x1b[O\x1b[I^C")
	got := string(stripFocusEvents(input))
	if got != "Press enter to continue^C" {
		t.Fatalf("focus events not stripped: %q", got)
	}
	// Ordinary input, including other escape sequences, must survive untouched.
	plain := "arrow:\x1b[C paste:\x1b[200~text\x1b[201~"
	if string(stripFocusEvents([]byte(plain))) != plain {
		t.Fatal("non-focus escapes were modified")
	}
	if string(stripFocusEvents([]byte("no escapes here"))) != "no escapes here" {
		t.Fatal("plain text was modified")
	}
}

// A child that asks for focus reporting keeps receiving it; until then the
// events are dropped.
func TestFocusStateOnlyForwardsWhatTheChildAskedFor(t *testing.T) {
	var state focusState
	noisy := []byte("prompt\x1b[I")
	if string(state.filter(noisy)) != "prompt" {
		t.Fatalf("focus events forwarded before the child asked: %q", state.filter(noisy))
	}

	state.observe([]byte("\x1b[?1004h"))
	if !state.enabled.Load() {
		t.Fatal("?1004h must enable focus forwarding")
	}
	if string(state.filter(noisy)) != string(noisy) {
		t.Fatal("focus events must reach a child that requested them")
	}

	state.observe([]byte("\x1b[?1004l"))
	if state.enabled.Load() {
		t.Fatal("?1004l must stop focus forwarding")
	}
	if strings.Contains(string(state.filter(noisy)), "\x1b[I") {
		t.Fatal("focus events must be dropped again after ?1004l")
	}
}
