package sandbox

import (
	"context"
	"strings"
	"testing"
)

// TestARunningRuntimeIsFoundHoweverItWasStarted. The npm package this image installs puts the model
// runtime behind an interpreter, and whether a sandbox shows `claude` or `node .../claude` depends on
// how the package was installed rather than on anything the system decides. A reader that knew one
// shape would call a live conversation an empty container on the other, which is the whole defect.
func TestARunningRuntimeIsFoundHoweverItWasStarted(t *testing.T) {
	for name, dump := range map[string]string{
		"started by name": "sleep infinity\n" +
			"claude --resume 0d4f2a --permission-mode acceptEdits\n",
		"started through an interpreter": "sleep infinity\n" +
			"/usr/bin/env node /usr/local/bin/claude --settings /home/agent/hooks/settings.json\n",
		"started by its full path": "/usr/local/bin/claude --session-id 0d4f2a\n",
		// What the container actually hands back: the kernel separates one process's arguments with a
		// zero byte, and nothing between /proc and here turns them into spaces.
		"as the kernel writes it, with zero bytes between the arguments": "sleep\x00infinity\x00\n" +
			"claude\x00--resume\x000d4f2a\x00--permission-mode\x00acceptEdits\x00\n",
		"under the conversation's terminal": "tmux new-session -A -s quay open-conversation 0d4f2a plan\n" +
			"/bin/sh /usr/local/bin/open-conversation 0d4f2a plan\n" +
			"claude --resume 0d4f2a --permission-mode plan\n",
	} {
		t.Run(name, func(t *testing.T) {
			if !runtimeAmong(dump) {
				t.Fatalf("the runtime is running and this reads the sandbox as empty:\n%s", dump)
			}
		})
	}
}

// TestASandboxWithNoRuntimeReadsEmpty. The other half, and the one that decides whether a reclaim
// can ever take a container back: a sandbox holding nothing must say so, or the system holds every
// container it ever made forever.
func TestASandboxWithNoRuntimeReadsEmpty(t *testing.T) {
	for name, dump := range map[string]string{
		"nothing but the container's own process": "sleep infinity\n",
		"a shell and a build": "sleep infinity\n" +
			"/bin/bash\n" +
			"go test ./...\n",
		// The conversation directory is named after the runtime and is on the command line of
		// anything that reads it. A path is not a program.
		"something reading the conversation directory": "sleep infinity\n" +
			"cat /home/agent/.claude/settings.json\n" +
			"ls /home/agent/.claude\n",
		"the package being installed": "npm install -g @anthropic-ai/claude-code@2.1.233\n",
		"nothing at all":              "",
	} {
		t.Run(name, func(t *testing.T) {
			if runtimeAmong(dump) {
				t.Fatalf("nothing is running and this reads the sandbox as busy:\n%s", dump)
			}
		})
	}
}

// TestTheProcessReaderDoesNotNameWhatItLooksFor. The trap this shape exists to avoid: the shell that
// reads the process table is itself a process in that container, so a reader carrying the runtime's
// name in its own command line finds itself and reports every sandbox in the system as busy. Nothing
// would ever be reclaimed again and every listing would say awake.
func TestTheProcessReaderDoesNotNameWhatItLooksFor(t *testing.T) {
	if strings.Contains(processTable, RuntimeBinary) {
		t.Fatalf("the process reader names %q, so it will find its own command line:\n%s",
			RuntimeBinary, processTable)
	}
	// And the proof, rather than the promise: the reader's own command line, read back the way the
	// container would show it, is not a runtime.
	if runtimeAmong("sh -c " + processTable) {
		t.Fatalf("the process reader reads itself as a running runtime:\n%s", processTable)
	}
}

// TestADaemonThatCannotBeReachedIsNotAnEmptySandbox. docker is not on the path here, so the command
// cannot be started at all, which is what an unreachable daemon looks like. The answer must be an
// error: a caller reads false as licence to close a container.
func TestADaemonThatCannotBeReachedIsNotAnEmptySandbox(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	running, err := DockerProvider{Image: "img"}.RuntimeRunning(context.Background(), "0d4f2a")
	if err == nil {
		t.Fatal("the daemon could not be reached and the system answered anyway")
	}
	if running {
		t.Fatal("a failure came back as a running runtime")
	}
	// The container it could not ask about, so an operator reading the log knows which session.
	if !strings.Contains(err.Error(), ContainerName("0d4f2a")) {
		t.Fatalf("the error does not name the container: %v", err)
	}
}

// TestTheLocalProviderRunsNothingOfItsOwn. A local sandbox is the host, so there is no per session
// process table to read. It answers the way it answers Attached, and it does not fail: a system running
// without containers must not fill its listing with sessions the system could not tell about.
func TestTheLocalProviderRunsNothingOfItsOwn(t *testing.T) {
	running, err := LocalProvider{}.RuntimeRunning(context.Background(), "0d4f2a")
	if err != nil {
		t.Fatalf("RuntimeRunning: %v", err)
	}
	if running {
		t.Fatal("the local provider claims a session's runtime is running")
	}
}

// TestTheFakeAnswersTheWayTheRealOneDoes. A double looser than the thing it stands in for
// manufactures green. The real provider asks a container that is not there and gets a non zero exit,
// which means nothing is running, so a session this provider never made must answer the same and a
// closed sandbox must stop answering.
func TestTheFakeAnswersTheWayTheRealOneDoes(t *testing.T) {
	ctx := context.Background()
	provider := &FakeProvider{}

	if running, err := provider.RuntimeRunning(ctx, "never-made"); running || err != nil {
		t.Fatalf("a session with no sandbox = (%v, %v), want (false, nil)", running, err)
	}

	if _, err := provider.Create(ctx, Config{ID: "0d4f2a"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if running, _ := provider.RuntimeRunning(ctx, "0d4f2a"); running {
		t.Fatal("a sandbox nobody woke reads as running a runtime")
	}

	provider.Wake("0d4f2a")
	if running, err := provider.RuntimeRunning(ctx, "0d4f2a"); !running || err != nil {
		t.Fatalf("a woken sandbox = (%v, %v), want (true, nil)", running, err)
	}

	if err := provider.Remove(ctx, "0d4f2a"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if running, _ := provider.RuntimeRunning(ctx, "0d4f2a"); running {
		t.Fatal("a sandbox that has gone still reads as running a runtime")
	}
}
