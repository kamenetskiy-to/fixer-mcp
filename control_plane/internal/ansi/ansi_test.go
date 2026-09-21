package ansi

import "testing"

func TestStrip(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{name: "csi color", in: "\x1b[31mred\x1b[0m", want: "red"},
		{name: "cursor move", in: "a\x1b[2Kb", want: "ab"},
		{name: "osc title", in: "\x1b]0;title\x07text", want: "text"},
		{name: "carriage returns dropped", in: "one\r\ntwo\r\n", want: "one\ntwo\n"},
		{name: "tabs kept", in: "a\tb", want: "a\tb"},
		{name: "control chars dropped", in: "a\x07b", want: "ab"},
		{name: "unterminated csi", in: "a\x1b[31", want: "a"},
		{name: "plain text", in: "plain", want: "plain"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Strip(tc.in); got != tc.want {
				t.Fatalf("Strip(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
