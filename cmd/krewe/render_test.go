package main

import (
	"context"
	"io"
	"strings"
	"testing"
)

// TestTheUsageNamesRender keeps the command and the manual from drifting apart. The manual is what a
// session is told the tool can do, so a command missing from it does not exist as far as the system is
// concerned.
func TestTheUsageNamesRender(t *testing.T) {
	if !strings.Contains(usage, "render <url>") {
		t.Error("the usage does not name krewe render, so no session is told it can look at what it built")
	}
}

// The command answers without the system. The page a session wants to see is one it is serving inside
// its own sandbox, so needing a control plane to look at it would be a reason not to look.
func TestRenderNeedsNoControlPlane(t *testing.T) {
	err := run(context.Background(), nil, []string{"render"}, io.Discard, "")

	if err == nil || !strings.Contains(err.Error(), "usage: krewe render") {
		t.Errorf("krewe render did not answer without a control plane: %v", err)
	}
}
