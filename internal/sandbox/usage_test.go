package sandbox_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/atlantic-blue/quay-krewe/internal/sandbox"
)

// transcript writes a conversation the way the model's command line tool does: one record per line,
// usage on the assistant's messages and nowhere else.
func transcript(t *testing.T, dir, workspace, conversation string, messages ...string) string {
	t.Helper()
	at := filepath.Join(dir, "workspaces", workspace, "claude", "projects", "-home-agent-workspace")
	if err := os.MkdirAll(at, 0o777); err != nil {
		t.Fatalf("make the transcript directory: %v", err)
	}
	path := filepath.Join(at, conversation+sandbox.ConversationFile)
	body := ""
	for _, line := range messages {
		body += line + "\n"
	}
	if err := os.WriteFile(path, []byte(body), 0o666); err != nil {
		t.Fatalf("write the transcript: %v", err)
	}
	return path
}

func assistant(in, out, cacheRead, cacheWritten int) string {
	return fmt.Sprintf(`{"type":"assistant","message":{"role":"assistant","usage":`+
		`{"input_tokens":%d,"output_tokens":%d,"cache_read_input_tokens":%d,`+
		`"cache_creation_input_tokens":%d}}}`, in, out, cacheRead, cacheWritten)
}

// TestWhatAConversationCostIsReadFromTheModelsOwnTranscript.
//
// The conversations that matter never go through the control plane: an operator talking in the panel
// is talking to the sandbox, and the only record is the file the tool writes as it goes.
func TestWhatAConversationCostIsReadFromTheModelsOwnTranscript(t *testing.T) {
	dir := t.TempDir()
	store := sandbox.Storage{Dir: dir, Host: dir}
	transcript(t, dir, "ws1", "c1",
		`{"type":"user","message":{"role":"user","content":"hello"}}`,
		assistant(12, 340, 1_000_000, 8_000),
		`{"type":"user","message":{"role":"user","content":"and again"}}`,
		assistant(40, 6_577, 723_404, 79_875),
	)

	got := store.ConversationUsage(sandbox.Config{Workspace: "ws1"}, "c1")
	want := sandbox.Usage{Input: 52, Output: 6_917, CacheRead: 1_723_404, CacheWritten: 87_875}
	if got != want {
		t.Fatalf("the conversation cost %+v, want %+v", got, want)
	}
	// Four numbers, not two. Reporting only what was sent and what came back would show 52 tokens on
	// this conversation and hide 1.7 million.
	if got.CacheRead <= got.Input {
		t.Fatal("the cache is not being counted, which is where nearly all of the cost is")
	}
}

// TestACostIsNeverWorthFailingAListingOver. A number is nice to have; a sessions listing that refuses
// to draw because a file is half written is not.
func TestACostIsNeverWorthFailingAListingOver(t *testing.T) {
	dir := t.TempDir()
	store := sandbox.Storage{Dir: dir, Host: dir}

	for _, tc := range []struct {
		name       string
		workspace  string
		conversati string
		write      func()
		want       sandbox.Usage
		because    string
	}{
		{
			name: "a conversation nobody has spoken in", workspace: "ws1", conversati: "never",
			because: "the system names a conversation before it exists, and that is not a failure",
		},
		{
			name: "a half written last line", workspace: "ws1", conversati: "c2",
			write: func() {
				transcript(t, dir, "ws1", "c2", assistant(10, 20, 30, 40), `{"type":"assis`)
			},
			want:    sandbox.Usage{Input: 10, Output: 20, CacheRead: 30, CacheWritten: 40},
			because: "the tool appends as it goes, so the last line of a live conversation is often torn",
		},
		{
			name: "a name that is not a name", workspace: "ws1", conversati: "../../../etc/passwd",
			because: "a handle from somewhere unexpected must not widen where this looks",
		},
		{
			name: "no workspace", conversati: "c1",
			because: "there is nowhere to look, which is not an error either",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.write != nil {
				tc.write()
			}
			if got := store.ConversationUsage(sandbox.Config{Workspace: tc.workspace}, tc.conversati); got != tc.want {
				t.Fatalf("it read %+v, want %+v, because %s", got, tc.want, tc.because)
			}
		})
	}
}

// TestACostIsCountedAgainWhenTheConversationMoves. The console refreshes every few seconds, so this is
// cached, and a cache that never notices a live conversation is worse than no number at all.
func TestACostIsCountedAgainWhenTheConversationMoves(t *testing.T) {
	dir := t.TempDir()
	store := sandbox.Storage{Dir: dir, Host: dir}
	path := transcript(t, dir, "ws1", "c3", assistant(1, 2, 3, 4))

	first := store.ConversationUsage(sandbox.Config{Workspace: "ws1"}, "c3")
	if first.Output != 2 {
		t.Fatalf("the first read is %+v", first)
	}

	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o666)
	if err != nil {
		t.Fatalf("append to the transcript: %v", err)
	}
	if _, err := file.WriteString(assistant(10, 20, 30, 40) + "\n"); err != nil {
		t.Fatalf("append to the transcript: %v", err)
	}
	_ = file.Close()
	// Some filesystems keep modification times to the second, so make the change unmistakable.
	later := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatalf("age the transcript: %v", err)
	}

	second := store.ConversationUsage(sandbox.Config{Workspace: "ws1"}, "c3")
	if second.Output != 22 {
		t.Fatalf("after the conversation moved it reads %+v, want the new messages counted", second)
	}
}
