package sandbox_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestOpeningAConversationResumesOneThatExistsAndStartsOneThatDoesNot runs the script the image
// ships, with a stand in for the model's command line tool, and reads back what it was asked to do.
//
// It matters that this is run rather than read. The crew names a conversation before anybody has
// spoken in it, so the name arrives at a sandbox with no transcript behind it, and resuming a name
// that is not there prints "No conversation found" and exits. From the console that looks like the
// key doing nothing at all.
func TestOpeningAConversationResumesOneThatExistsAndStartsOneThatDoesNot(t *testing.T) {
	const conversation = "0b4f2f7c-2f0e-4a1e-8a2a-9a6f1b3c5d7e"

	for _, tc := range []struct {
		name       string
		transcript bool
		id         string
		want       string
		absent     string
		because    string
	}{
		{
			name: "a conversation the crew has named but nobody has opened", id: conversation,
			want: "--session-id " + conversation, absent: "--resume",
			because: "starting it under that name is what makes the name true",
		},
		{
			name: "a conversation with a transcript behind it", id: conversation, transcript: true,
			want: "--resume " + conversation, absent: "--session-id",
			because: "it is the same conversation and the operator expects their history",
		},
		{
			name: "no name at all", id: "", absent: "--session-id",
			because: "an unnamed conversation is the model's to name, which is how this worked before",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			asked := filepath.Join(home, "asked")
			fake(t, home, asked)

			if tc.transcript {
				dir := filepath.Join(home, ".claude", "projects", "-home-agent-workspace")
				if err := os.MkdirAll(dir, 0o777); err != nil {
					t.Fatalf("make the transcript directory: %v", err)
				}
				if err := os.WriteFile(filepath.Join(dir, conversation+".jsonl"), []byte("{}\n"), 0o666); err != nil {
					t.Fatalf("write the transcript: %v", err)
				}
			}

			run(t, home, tc.id, asked)

			said, err := os.ReadFile(asked)
			if err != nil {
				t.Fatalf("the script never ran the model: %v", err)
			}
			got := strings.TrimSpace(string(said))
			if tc.want != "" && !strings.Contains(got, tc.want) {
				t.Errorf("the model was asked %q, want it to carry %q, because %s", got, tc.want, tc.because)
			}
			if tc.absent != "" && strings.Contains(got, tc.absent) {
				t.Errorf("the model was asked %q, and it should not carry %q, because %s", got, tc.absent, tc.because)
			}
		})
	}
}

// fake puts a stand in for the model's command line tool on the path, which records what it was asked
// and returns, so the script moves on to the line after it.
func fake(t *testing.T, home, asked string) {
	t.Helper()
	claude := filepath.Join(home, "claude")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> " + asked + "\n"
	if err := os.WriteFile(claude, []byte(script), 0o755); err != nil {
		t.Fatalf("write the stand in: %v", err)
	}
}

// run executes the script the image ships, and stops it once it has asked for a conversation. It
// loops forever by design, so that a conversation ending does not take the terminal with it.
func run(t *testing.T, home, conversation, asked string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	command := exec.CommandContext(ctx, "sh", "../../deploy/sandbox/open-conversation.sh", conversation, "acceptEdits")
	command.Env = append(os.Environ(), "HOME="+home, "PATH="+home+":"+os.Getenv("PATH"))
	command.Stdin = strings.NewReader("")
	if err := command.Start(); err != nil {
		t.Fatalf("start the script: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()

	// It asks the model, prints, and then waits on a key that is never coming, which is what the loop
	// is for: a conversation ending must not take the terminal with it. So this waits for the answer
	// to arrive rather than for a duration, and stops the script once it has.
	defer func() {
		_ = command.Process.Kill()
		<-done
	}()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(asked); err == nil {
			return
		}
		select {
		case err := <-done:
			if err != nil && !errors.Is(ctx.Err(), context.DeadlineExceeded) {
				t.Logf("the script exited before asking for a conversation: %v", err)
			}
			return
		case <-time.After(10 * time.Millisecond):
		}
	}
	t.Fatal("the script never asked for a conversation")
}
