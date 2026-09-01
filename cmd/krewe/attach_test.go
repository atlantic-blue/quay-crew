package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
)

func TestAttachCommandOpensTheConversationInTheSandbox(t *testing.T) {
	spec := &quaycrewv1.AttachSessionResponse{
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
	if _, err := attachCommand(&quaycrewv1.AttachSessionResponse{}); err == nil {
		t.Fatal("attaching with no sandbox or command succeeded")
	}
}

// The defect this exists for: attach is usually the whole command of a tmux pane, and a pane closes
// the moment its command exits. A refusal printed the reason and lost it in the same instant, so the
// operator pressed a key, the screen flickered, and nothing said why.
func TestAttachSaysWhyItCannotOpenAndWaitsThere(t *testing.T) {
	client, _ := aSessionWatchingTheModel(t)
	var screen bytes.Buffer
	// Nothing has been typed yet, so a reader that is not at its end holds the command where the
	// operator can read it. The pane is what would have closed.
	held, operator := io.Pipe()
	defer func() { _ = held.Close() }()

	done := make(chan error, 1)
	go func() { done <- runAttach(context.Background(), client, []string{"ffffffff"}, &screen, held) }()

	waitFor(t, func() bool { return strings.Contains(screen.String(), "Press enter") })
	select {
	case err := <-done:
		t.Fatalf("attach exited before the operator read anything: %v", err)
	default:
	}

	// And the next key: enter gives them their terminal back.
	go func() { _, _ = operator.Write([]byte("\n")) }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("attaching to a session that does not exist reported success")
		}
		if !errors.Is(err, ErrSaid) {
			t.Fatalf("the failure is %v, want one already said to the operator", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("attach never came back after enter was pressed")
	}

	said := screen.String()
	if !strings.Contains(said, "ffffffff") {
		t.Fatalf("the screen says %q, want it to name what was typed", said)
	}
}

// Nothing is attached to a pipeline's standard input, so a scripted attach reads the end of it and
// comes back rather than hanging on a terminal that is not there.
func TestAttachDoesNotHangWhenThereIsNoTerminal(t *testing.T) {
	client, _ := aSessionWatchingTheModel(t)
	var screen bytes.Buffer

	done := make(chan error, 1)
	go func() {
		done <- runAttach(context.Background(), client, []string{"ffffffff"}, &screen, strings.NewReader(""))
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("attaching to a session that does not exist reported success")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("attach hung on a standard input with nothing attached to it")
	}
}

// waitFor gives a goroutine a moment to reach a state, so a test says what it is waiting for rather
// than sleeping a guess.
func waitFor(t *testing.T, done func() bool) {
	t.Helper()
	for range 500 {
		if done() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the condition never came true")
}
