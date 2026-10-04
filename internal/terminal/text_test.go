package terminal

import "testing"

func TestSafe(t *testing.T) {
	got := Safe("repo\x1b[31m\n\u202ehi\u2069")
	if got != "repo [31m  hi " {
		t.Fatalf("unexpected sanitized text: %q", got)
	}
}
