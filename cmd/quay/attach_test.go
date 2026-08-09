package main

import (
	"strings"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
)

func TestAttachCommandOpensTheConversationInTheSandbox(t *testing.T) {
	spec := &quaycrewv1.AttachThreadResponse{
		Sandbox: "quaycrew-abc123",
		Argv:    []string{"claude", "--resume", "conversation-1"},
	}

	command, err := attachCommand(spec)
	if err != nil {
		t.Fatalf("attachCommand: %v", err)
	}

	line := strings.Join(command.Args, " ")
	for _, want := range []string{"docker exec", "--interactive", "--tty", "quaycrew-abc123", "claude --resume conversation-1"} {
		if !strings.Contains(line, want) {
			t.Fatalf("command %q is missing %q", line, want)
		}
	}
	// The credential lives on the sandbox, so this command must not carry one.
	if strings.Contains(line, "CLAUDE_CODE_OAUTH_TOKEN") {
		t.Fatalf("the attach command carries a credential: %q", line)
	}
}

func TestAttachRefusesAnEmptySpecification(t *testing.T) {
	if _, err := attachCommand(&quaycrewv1.AttachThreadResponse{}); err == nil {
		t.Fatal("attaching with no sandbox or command succeeded")
	}
}
