package main

import (
	"strings"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
)

func TestAttachCommandOpensTheConversationInTheSandbox(t *testing.T) {
	spec := &quaycrewv1.AttachSessionResponse{
		Sandbox: "quaycrew-abc123",
		Argv:    []string{"claude", "--resume", "conversation-1"},
	}

	command, err := attachCommand(spec, "tok-xyz")
	if err != nil {
		t.Fatalf("attachCommand: %v", err)
	}

	line := strings.Join(command.Args, " ")
	for _, want := range []string{"docker exec", "--interactive", "--tty", "quaycrew-abc123", "claude --resume conversation-1"} {
		if !strings.Contains(line, want) {
			t.Fatalf("command %q is missing %q", line, want)
		}
	}
	// Without a terminal the model has nothing to attach to, so both flags matter.
	if !strings.Contains(line, "CLAUDE_CODE_OAUTH_TOKEN=tok-xyz") {
		t.Fatalf("command %q does not carry the token", line)
	}
}

func TestAttachRefusesWithoutAToken(t *testing.T) {
	spec := &quaycrewv1.AttachSessionResponse{Sandbox: "quaycrew-abc123", Argv: []string{"claude"}}

	_, err := attachCommand(spec, "  ")
	if err == nil {
		t.Fatal("attaching without a token succeeded")
	}
	// The message has to say what to set, or it reads as a broken tool.
	if !strings.Contains(err.Error(), "CLAUDE_CODE_OAUTH_TOKEN") {
		t.Fatalf("the error %q does not name the setting", err)
	}
}

func TestAttachRefusesAnEmptySpecification(t *testing.T) {
	if _, err := attachCommand(&quaycrewv1.AttachSessionResponse{}, "tok"); err == nil {
		t.Fatal("attaching with no sandbox or command succeeded")
	}
}
