package main

import (
	"bytes"
	"strings"
	"testing"
)

// TestTheManualIsOneThingToPipe: it takes no arguments, because it is a document, and anything typed
// after it is somebody expecting it to be a different command.
func TestTheManualIsOneThingToPipe(t *testing.T) {
	if err := runManual([]string{"sessions"}, &bytes.Buffer{}); err == nil {
		t.Fatal("quay manual took an argument")
	}
}

// TestTheManualCarriesTheRealCommands, the same text `quay` prints with no arguments, so a command
// renamed in one place cannot go on being described in the other.
func TestTheManualCarriesTheRealCommands(t *testing.T) {
	var out bytes.Buffer
	if err := runManual(nil, &out); err != nil {
		t.Fatalf("runManual: %v", err)
	}
	if !strings.Contains(out.String(), strings.TrimSpace(usage)) {
		t.Fatal("the manual does not carry the tool's own usage")
	}
	// And the manual is what teaches a session, so it must mention the command that does the teaching.
	if !strings.Contains(out.String(), "quay context set") {
		t.Fatal("the manual never says how to set a context")
	}
}
