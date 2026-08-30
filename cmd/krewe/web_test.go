package main

import (
	"context"
	"io"
	"strings"
	"testing"
)

// TestTheUsageNamesWeb keeps the command and the manual from drifting apart. The manual is what a
// session is told the tool can do, so a command missing from it does not exist as far as the system is
// concerned.
func TestTheUsageNamesWeb(t *testing.T) {
	if !strings.Contains(usage, "web [<address>]") {
		t.Error("the usage does not name krewe web, so nobody is told it is there")
	}
}

func TestWebTakesAtMostAnAddress(t *testing.T) {
	client := testClient(t)

	err := run(context.Background(), client, []string{"web", "127.0.0.1:8080", "and-one-more"}, io.Discard, "")

	if err == nil {
		t.Fatal("krewe web took two addresses without complaint")
	}
	if !strings.Contains(err.Error(), "usage: krewe web") {
		t.Errorf("the refusal does not say how to call it: %v", err)
	}
}

// TestWebRefusesAnAddressBeyondThisMachine drives the refusal through the command rather than the
// server, because the operator meets it here. The rule itself is held in internal/web.
func TestWebRefusesAnAddressBeyondThisMachine(t *testing.T) {
	client := testClient(t)

	err := run(context.Background(), client, []string{"web", "0.0.0.0:8080"}, io.Discard, "")

	if err == nil {
		t.Fatal("krewe web served an address that is reachable from another machine")
	}
	if !strings.Contains(err.Error(), "this machine only") {
		t.Errorf("the refusal does not say why: %v", err)
	}
}
