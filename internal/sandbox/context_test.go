package sandbox_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atlantic-blue/krewe/internal/sandbox"
)

// answer is one record of a transcript, as the model's command line tool writes it.
func answer(in, out, cacheRead, cacheWritten int64) string {
	return fmt.Sprintf(`{"type":"assistant","isSidechain":false,"message":{"role":"assistant","usage":`+
		`{"input_tokens":%d,"output_tokens":%d,"cache_read_input_tokens":%d,`+
		`"cache_creation_input_tokens":%d}}}`, in, out, cacheRead, cacheWritten)
}

// subAgentAnswer is a record belonging to a sub agent, which fills a window of its own.
func subAgentAnswer(in, cacheRead int64) string {
	return fmt.Sprintf(`{"type":"assistant","isSidechain":true,"message":{"role":"assistant","usage":`+
		`{"input_tokens":%d,"output_tokens":10,"cache_read_input_tokens":%d,`+
		`"cache_creation_input_tokens":0}}}`, in, cacheRead)
}

const askedSomething = `{"type":"user","message":{"role":"user","content":"and now?"}}`

// wroteTranscript puts a conversation on disk where the system reads it, and answers with the storage
// and the names to read it back by.
func wroteTranscript(t *testing.T, lines ...string) (sandbox.Storage, string, string) {
	t.Helper()
	const workspace, conversation = "0b4f2f7c2f0e4a1e8a2a9a6f", "1c4f2f7c-2f0e-4a1e-8a2a-9a6f1b3c5d7e"
	dir := t.TempDir()
	at := filepath.Join(dir, "workspaces", workspace, "claude", "projects", "-home-agent-workspace")
	if err := os.MkdirAll(at, 0o777); err != nil {
		t.Fatalf("make the transcript directory: %v", err)
	}
	body := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(at, conversation+sandbox.ConversationFile), []byte(body), 0o666); err != nil {
		t.Fatalf("write the transcript: %v", err)
	}
	return sandbox.Storage{Dir: dir, Host: dir}, workspace, conversation
}

// How full the window is is not what the conversation cost. Cost only grows; the window is whatever
// the last answer carried, and it empties again when the model compacts. Reading the cost into the
// column would report every long conversation as far past full.
func TestTheContextIsTheLastAnswerRatherThanTheWholeConversation(t *testing.T) {
	for _, tc := range []struct {
		name    string
		lines   []string
		want    int64
		because string
	}{
		{
			name:    "one answer",
			lines:   []string{answer(52, 400, 124_000, 300)},
			want:    124_352,
			because: "everything sent is the context, and almost all of it arrives from the cache",
		},
		{
			name:    "several answers",
			lines:   []string{answer(52, 400, 100_000, 0), askedSomething, answer(60, 500, 180_000, 40)},
			want:    180_100,
			because: "the last answer carried everything before it, so only the last one counts",
		},
		{
			name:    "a conversation the model compacted",
			lines:   []string{answer(52, 400, 900_000, 0), askedSomething, answer(10, 200, 20_000, 0)},
			want:    20_010,
			because: "compacting empties the window, and a column that only ever grows would say the opposite",
		},
		{
			name:    "a sub agent answered last",
			lines:   []string{answer(52, 400, 100_000, 0), subAgentAnswer(30, 700_000)},
			want:    100_052,
			because: "a sub agent fills a window of its own, and the operator is still typing into this one",
		},
		{
			name:    "the last line is half written",
			lines:   []string{answer(52, 400, 100_000, 0), `{"type":"assistant","mess`},
			want:    100_052,
			because: "the tool writes the transcript as it goes, so the last line of a live conversation is regularly half a line",
		},
		{
			name:    "nobody has spoken",
			lines:   []string{askedSomething},
			want:    0,
			because: "a conversation with no answer in it has filled nothing",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			storage, workspace, conversation := wroteTranscript(t, tc.lines...)
			if got := storage.ConversationContext(sandbox.Config{Workspace: workspace}, conversation).Carried(); got != tc.want {
				t.Errorf("the window holds %d, want %d\n\n%s", got, tc.want, tc.because)
			}
		})
	}
}

// The cost column and the context column read the same file in one pass, so neither can be right
// while the other is stale.
func TestTheCostIsStillTheWholeConversation(t *testing.T) {
	storage, workspace, conversation := wroteTranscript(t,
		answer(52, 400, 100_000, 0), askedSomething, answer(60, 500, 180_000, 40))

	spent := storage.ConversationUsage(sandbox.Config{Workspace: workspace}, conversation)
	if want := int64(112); spent.Input != want {
		t.Errorf("the conversation cost %d in, want %d", spent.Input, want)
	}
	if want := int64(900); spent.Output != want {
		t.Errorf("the conversation cost %d out, want %d", spent.Output, want)
	}
	if carried := storage.ConversationContext(sandbox.Config{Workspace: workspace}, conversation).Carried(); carried != 180_100 {
		t.Errorf("the window holds %d, want 180100", carried)
	}
}

// The system cannot work the window size out for itself. A session writes down what the model runtime
// told it, and the system reads that, because a list of models in the code is right today and quietly
// wrong at the next one.
func TestTheWindowSizeIsWhateverASessionWroteDown(t *testing.T) {
	storage, workspace, _ := wroteTranscript(t, answer(52, 400, 100_000, 0))
	at := filepath.Join(storage.Dir, "workspaces", workspace, "claude", sandbox.ContextWindowFile)

	if _, said := storage.ContextWindowSize(workspace); said {
		t.Error("the system claims to know the window size before anything told it")
	}

	for _, tc := range []struct {
		name  string
		wrote string
		want  int64
		said  bool
	}{
		{name: "a size", wrote: "1000000\n", want: 1_000_000, said: true},
		{name: "a size with no line ending", wrote: "200000", want: 200_000, said: true},
		{name: "nothing", wrote: "", said: false},
		{name: "not a number", wrote: "one million\n", said: false},
		{name: "a size of nothing", wrote: "0\n", said: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(at, []byte(tc.wrote), 0o666); err != nil {
				t.Fatalf("write the size: %v", err)
			}
			size, said := storage.ContextWindowSize(workspace)
			if said != tc.said || size != tc.want {
				t.Errorf("the system reads %d (said=%v), want %d (said=%v)", size, said, tc.want, tc.said)
			}
		})
	}
}

// A role session keeps its own conversation store, so the window has to be read from there. Read from
// the workspace's store instead and a full window answers empty, which reads as a conversation nobody
// has spoken in rather than as one that is nearly out of room.
func TestARoleSessionsWindowIsReadFromItsOwnStore(t *testing.T) {
	const workspace, project, session = "0b4f2f7c2f0e4a1e8a2a9a6f", "3d5e", "7a9b"
	const conversation = "1c4f2f7c-2f0e-4a1e-8a2a-9a6f1b3c5d7e"
	dir := t.TempDir()
	at := filepath.Join(dir, "workspaces", workspace, "projects", project, "sessions", session,
		"claude", "projects", "-home-agent-workspace")
	if err := os.MkdirAll(at, 0o777); err != nil {
		t.Fatalf("make the transcript directory: %v", err)
	}
	body := answer(52, 400, 180_000, 100) + "\n"
	if err := os.WriteFile(filepath.Join(at, conversation+sandbox.ConversationFile), []byte(body), 0o666); err != nil {
		t.Fatalf("write the transcript: %v", err)
	}
	storage := sandbox.Storage{Dir: dir, Host: dir}

	asTheRole := sandbox.Config{ID: session, Workspace: workspace, Project: project, Role: "test-writer"}
	if carried := storage.ConversationContext(asTheRole, conversation).Carried(); carried != 180_152 {
		t.Errorf("the role session's window holds %d, want 180152", carried)
	}
	asTheWorkspace := sandbox.Config{ID: session, Workspace: workspace, Project: project}
	if carried := storage.ConversationContext(asTheWorkspace, conversation).Carried(); carried != 0 {
		t.Errorf("the workspace's store answered %d for a role session's conversation, want 0", carried)
	}
}
