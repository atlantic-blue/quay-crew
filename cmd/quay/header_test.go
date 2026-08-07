package main

import (
	"bytes"
	"strings"
	"testing"
)

// TestTheHeaderNeverScrolls. Redrawing printed after what was already there, so every second added
// another header to the pane's history and the pane scrolled: seventeen of them stacked up the screen.
//
// Two things stop it. The alternate screen, so redrawing never touches the scrollback at all, and
// homing the cursor and clearing rather than printing on the end.
func TestTheHeaderNeverScrolls(t *testing.T) {
	var out bytes.Buffer
	painted := paintHeader(&out, []string{" Version: dev", " second line"}, 80, true)
	if painted != nil {
		t.Fatalf("paintHeader: %v", painted)
	}
	drawn := out.String()

	// The escape sequence written out, not the constant: comparing a constant with itself passes
	// however the constant is changed, which is a test that watches nothing.
	if !strings.Contains(drawn, "\033[?1049h") {
		t.Fatalf("the first frame does not enter the alternate screen, so redraws become history:\n%q", drawn)
	}
	if !strings.Contains(drawn, "\033[H") || !strings.Contains(drawn, "\033[2J") {
		t.Fatalf("the header does not home the cursor and clear, so it prints on the end:\n%q", drawn)
	}
	if strings.HasSuffix(drawn, "\n") {
		t.Fatalf("the header ends with a newline, which scrolls a pane it exactly fills:\n%q", drawn)
	}
	// Lines are separated by a carriage return and a newline, which moves rather than scrolls.
	if strings.Count(drawn, "\r\n") != 1 {
		t.Fatalf("two lines are not joined by a carriage return:\n%q", drawn)
	}
}

// TestTheHeaderIsCutAColumnShortOfThePane: a line reaching the last column wraps in some terminals,
// and a wrapped line pushes the one below it out of a pane three rows tall.
func TestTheHeaderIsCutAColumnShortOfThePane(t *testing.T) {
	var out bytes.Buffer
	long := strings.Repeat("x", 200)
	if err := paintHeader(&out, []string{long}, 80, false); err != nil {
		t.Fatalf("paintHeader: %v", err)
	}
	if strings.Count(out.String(), "x") > 79 {
		t.Fatalf("the header fills the pane to its last column: %d", strings.Count(out.String(), "x"))
	}
}
