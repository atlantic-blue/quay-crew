package sandbox_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-krewe/internal/contextspend"
	"github.com/atlantic-blue/quay-krewe/internal/sandbox"
)

// The records a transcript is made of, written the way the model's command line tool writes them.
// The tests below build conversations out of these rather than out of hand written JSON, so a test
// says what happened in the conversation and not what the file looked like.

// asked is a person or the system putting something in front of the session.
func asked(text string) string {
	return record("user", false, map[string]any{"role": "user", "content": text})
}

// wrote is the session answering in its own words, with the usage the model reported.
func wrote(text string, carried int64) string {
	return record("assistant", false, map[string]any{
		"role":    "assistant",
		"content": []any{map[string]any{"type": "text", "text": text}},
		"usage": map[string]any{
			"input_tokens": carried, "output_tokens": 100,
			"cache_read_input_tokens": 0, "cache_creation_input_tokens": 0,
		},
	})
}

// thought is the session thinking, which is its own words and costs the window the same as any other.
func thought(text string) string {
	return record("assistant", false, map[string]any{
		"role":    "assistant",
		"content": []any{map[string]any{"type": "thinking", "thinking": text}},
	})
}

// called is the session asking for a tool. The arguments are its own words, and the call is where the
// tool's name is recorded, because the result carries no name at all.
func called(id, tool string, input map[string]any) string {
	return record("assistant", false, map[string]any{
		"role": "assistant",
		"content": []any{map[string]any{
			"type": "tool_use", "id": id, "name": tool, "input": input,
		}},
	})
}

// returned is what a tool handed back, which arrives on a user record.
func returned(id, text string) string {
	return record("user", false, map[string]any{
		"role": "user",
		"content": []any{map[string]any{
			"type": "tool_result", "tool_use_id": id, "content": text,
		}},
	})
}

// subAgentRead is a sub agent reading a file. It fills a window of its own, so none of it belongs to
// this conversation.
func subAgentRead(id, text string) string {
	call := record("assistant", true, map[string]any{
		"role": "assistant",
		"content": []any{map[string]any{
			"type": "tool_use", "id": id, "name": "Read", "input": map[string]any{"file_path": "/x"},
		}},
	})
	result := record("user", true, map[string]any{
		"role": "user",
		"content": []any{map[string]any{
			"type": "tool_result", "tool_use_id": id, "content": text,
		}},
	})
	return call + "\n" + result
}

func record(kind string, sidechain bool, message map[string]any) string {
	line, err := json.Marshal(map[string]any{
		"type": kind, "isSidechain": sidechain, "message": message,
	})
	if err != nil {
		panic(err)
	}
	return string(line)
}

// spendOf writes a conversation and reads back where its context went.
func spendOf(t *testing.T, lines ...string) contextspend.Spend {
	t.Helper()
	storage, workspace, conversation := wroteTranscript(t, lines...)
	return storage.ConversationSpend(sandbox.Config{Workspace: workspace}, conversation)
}

// A conversation nobody has spoken in, and one this cannot find at all. Both have filled nothing,
// and neither is a conversation that spent nothing.
func TestAConversationWithNoTranscriptSpentNothing(t *testing.T) {
	storage, workspace, conversation := wroteTranscript(t, asked("hello"))
	for _, tc := range []struct {
		name                    string
		workspace, conversation string
	}{
		{"a conversation that is not there", workspace, "9f4f2f7c-2f0e-4a1e-8a2a-9a6f1b3c5d7e"},
		{"a workspace that is not there", "0000000000000000000000ff", conversation},
		{"no conversation named at all", workspace, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := storage.ConversationSpend(sandbox.Config{Workspace: tc.workspace}, tc.conversation)
			if !got.Empty() {
				t.Errorf("it reports %+v, want nothing at all", got)
			}
		})
	}
}

// The tool writes the file as it goes, so the last line of a live conversation is regularly half
// written. Losing the whole breakdown over that would mean the number only ever appeared when
// nothing was happening.
func TestAHalfWrittenLastLineDoesNotLoseTheRest(t *testing.T) {
	whole := spendOf(t, asked("read the controller"), returnedRead("t1", 4_000))
	half := spendOf(t, asked("read the controller"), returnedRead("t1", 4_000),
		`{"type":"assistant","isSidechain":false,"message":{"role":"assist`)
	if half != whole {
		t.Errorf("a half written last line changed the breakdown from %+v to %+v", whole, half)
	}
	if half.Reads == 0 {
		t.Error("the read before the half written line was lost")
	}
}

// A sub agent has a context window of its own. Counting its reads here would say this conversation
// filled up on a file it never saw.
func TestASubAgentsReadingIsNotThisConversationsSpend(t *testing.T) {
	alone := spendOf(t, asked("go and look"), wrote("done", 900))
	withSubAgent := spendOf(t, asked("go and look"),
		subAgentRead("sub1", strings.Repeat("x", 50_000)), wrote("done", 900))
	if withSubAgent != alone {
		t.Errorf("a sub agent's reading landed on this conversation: %+v against %+v",
			withSubAgent, alone)
	}
}

// A result whose call this never saw. It is tool output, because the alternative is to drop it and a
// total that drops what it did not understand is the number this measurement exists to avoid.
func TestAResultWithNoCallBehindItIsStillCounted(t *testing.T) {
	spent := spendOf(t, returned("never-seen", strings.Repeat("x", 1_000)))
	if spent.Total() != 1_000 {
		t.Fatalf("the total is %d, want the 1,000 characters that came back", spent.Total())
	}
	if spent.Tools != 1_000 {
		t.Errorf("tools holds %d of them, want all 1,000: an unnamed result is not a file read",
			spent.Tools)
	}
}

// A record shaped like nothing this knows. Its characters are still in the window, so they are still
// in the total.
func TestARecordThisDoesNotUnderstandIsStillCounted(t *testing.T) {
	odd := `{"type":"user","isSidechain":false,"message":{"role":"user","content":` +
		`{"unexpected":"` + strings.Repeat("y", 200) + `"}}}`
	spent := spendOf(t, odd)
	if spent.Total() < 200 {
		t.Errorf("the total is %d, want at least the 200 characters the record holds", spent.Total())
	}
	if spent.Told != spent.Total() {
		t.Errorf("told holds %d of the %d, want all of them", spent.Told, spent.Total())
	}
}

// The whole conversation, split four ways, and the parts adding up to it. The recount is done from
// the lines the test wrote rather than from the four fields, so a reader that quietly dropped a block
// would fail here rather than report a tidy total.
func TestEveryCharacterOfAConversationLandsInOneCategory(t *testing.T) {
	const (
		exec     = "read the controller and say what it does"
		thinking = "the controller is where the loop lives"
		answer   = "it reconciles jobs"
	)
	file := strings.Repeat("a", 8_000)
	output := strings.Repeat("b", 3_000)

	spent := spendOf(t,
		asked(exec),
		thought(thinking),
		called("t1", "Read", map[string]any{"file_path": "/repo/controller.go"}),
		returned("t1", file),
		called("t2", "Bash", map[string]any{"command": "go test ./..."}),
		returned("t2", output),
		wrote(answer, 12_000),
	)

	calls := len(`Read`) + len(`{"file_path":"/repo/controller.go"}`) +
		len(`Bash`) + len(`{"command":"go test ./..."}`)
	want := contextspend.Spend{
		Reads: int64(len(file)),
		Tools: int64(len(output)),
		Turns: int64(len(thinking) + len(answer) + calls),
		Told:  int64(len(exec)),
	}
	if spent != want {
		t.Fatalf("the breakdown is %+v, want %+v", spent, want)
	}
	if spent.Total() != want.Reads+want.Tools+want.Turns+want.Told {
		t.Errorf("the total is %d and the four parts add up to %d",
			spent.Total(), want.Reads+want.Tools+want.Turns+want.Told)
	}
}

// The reason the shell rule exists. A session told to work through the shell reads its files with
// `cat`, and without this those characters are tool output and the reads column reads nothing.
func TestAFileReadThroughTheShellIsARead(t *testing.T) {
	file := strings.Repeat("a", 6_000)
	spent := spendOf(t,
		called("t1", "Bash", map[string]any{"command": "cat internal/job/controller.go"}),
		returned("t1", file),
	)
	if spent.Reads != int64(len(file)) {
		t.Errorf("reads holds %d characters, want the %d the file came back with", spent.Reads, len(file))
	}
	if spent.Tools != 0 {
		t.Errorf("tools holds %d characters of a file read", spent.Tools)
	}
}

// A shell command that is not reading a file. The same tool, the same shape of result, and the other
// column, because the command decides and not the tool.
func TestShellOutputThatIsNotAFileIsToolOutput(t *testing.T) {
	output := strings.Repeat("b", 6_000)
	spent := spendOf(t,
		called("t1", "Bash", map[string]any{"command": "go test ./..."}),
		returned("t1", output),
	)
	if spent.Tools != int64(len(output)) {
		t.Errorf("tools holds %d characters, want the %d the command printed", spent.Tools, len(output))
	}
	if spent.Reads != 0 {
		t.Errorf("reads holds %d characters of a test run", spent.Reads)
	}
}

// What a tool returns arrives as a string on most tools and as a list of blocks on the ones that hand
// back more than text. Both are the same output to the window.
func TestAResultWrittenAsBlocksIsMeasuredTheSameWay(t *testing.T) {
	text := strings.Repeat("a", 2_000)
	asBlocks := record("user", false, map[string]any{
		"role": "user",
		"content": []any{map[string]any{
			"type": "tool_result", "tool_use_id": "t1",
			"content": []any{map[string]any{"type": "text", "text": text}},
		}},
	})
	spent := spendOf(t,
		called("t1", "Read", map[string]any{"file_path": "/repo/x.go"}),
		asBlocks,
	)
	if spent.Reads != int64(len(text)) {
		t.Errorf("reads holds %d characters, want the %d in the block", spent.Reads, len(text))
	}
}

// The breakdown comes out of the same pass that counts the cost and the window, so a listing does not
// read every transcript in the system three times over. A conversation that has not changed answers
// from what was kept.
func TestTheBreakdownIsKeptWithTheCostAndTheWindow(t *testing.T) {
	storage, workspace, conversation := wroteTranscript(t,
		called("t1", "Read", map[string]any{"file_path": "/repo/x.go"}),
		returned("t1", strings.Repeat("a", 5_000)),
		wrote("done", 4_000),
	)
	cfg := sandbox.Config{Workspace: workspace}
	first := storage.ConversationSpend(cfg, conversation)
	if first.Reads != 5_000 {
		t.Fatalf("reads holds %d characters, want 5,000", first.Reads)
	}
	if second := storage.ConversationSpend(cfg, conversation); second != first {
		t.Errorf("a second reading of an unchanged conversation says %+v, and the first said %+v",
			second, first)
	}
	if carried := storage.ConversationContext(cfg, conversation).Carried(); carried != 4_000 {
		t.Errorf("the window says %d, want the 4,000 the last answer carried: the breakdown must not "+
			"disturb the two readings it shares a pass with", carried)
	}
}

// returnedRead is a file read and what came back, as one pair of records.
func returnedRead(id string, characters int) string {
	return called(id, "Read", map[string]any{"file_path": "/repo/x.go"}) + "\n" +
		returned(id, strings.Repeat("a", characters))
}

// The measurement in docs/CONTEXT-SPEND.md, held against the accounting that produced it. It is here
// rather than in a document alone so the ratio cannot drift away from the number the document quotes.
func TestTheMeasuredRatioIsWhatTheDocumentSays(t *testing.T) {
	// 40,513,562 characters against 21,474,417 tokens is 1.887, which the code rounds to 1.9.
	if got := contextspend.Tokens(1_900); got != 1_000 {
		t.Errorf("1,900 characters comes to %d tokens, want 1,000 at 1.9 characters a token", got)
	}
	if got := contextspend.Tokens(0); got != 0 {
		t.Errorf("nothing comes to %d tokens", got)
	}
}
