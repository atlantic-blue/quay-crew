package sandbox

import (
	"encoding/json"
	"unicode/utf8"

	"github.com/atlantic-blue/quay-krewe/internal/contextspend"
)

// contentBlock is one piece of a message. A message is either a plain string, which is somebody
// typing, or a list of these.
//
// One shape for every kind of block, because the fields do not collide: text carries Text, thinking
// carries Thinking, a call carries Name and Input, and a result carries ToolUseID and Content. A
// block this does not recognise still has a length, and a length is what the accounting needs.
type contentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	Thinking  string          `json:"thinking"`
	Name      string          `json:"name"`
	ID        string          `json:"id"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	// Content is what a tool returned, and it has the same two shapes a message has: a string, or a
	// list of blocks with text in them.
	Content json.RawMessage `json:"content"`
}

// call is one tool the session asked for. A result carries neither the tool's name nor its arguments,
// so both are kept from the call and read back when the result arrives.
type call struct {
	tool string
	// command is what the session asked the shell to run, and empty for every other tool. It is what
	// decides whether the output that came back is the contents of a file.
	command string
}

// shellCall is the part of a call to the shell the accounting reads.
type shellCall struct {
	Command string `json:"command"`
}

// countSpend puts one transcript record into the accounting, and records the name of any tool the
// session called so the result can be read back later.
//
// Every character it walks lands in exactly one category. A record it does not recognise is counted
// as told rather than skipped: a total that quietly drops what it did not understand is the shape of
// number this whole measurement exists to avoid.
func countSpend(into *contextspend.Spend, record transcriptLine, calls map[string]call) {
	body := record.Message.Content
	if len(body) == 0 {
		return
	}

	// A message written as one string is somebody typing into the session, or the session answering
	// in one piece. Which of the two is what the record type says.
	var typed string
	if json.Unmarshal(body, &typed) == nil {
		into.Count(categoryOfRecord(record.Type), int64(utf8.RuneCountInString(typed)))
		return
	}

	var blocks []contentBlock
	if json.Unmarshal(body, &blocks) != nil {
		// Neither shape. It is still context, and its size is still the file's, so it is counted
		// rather than lost.
		into.Count(contextspend.Told, int64(utf8.RuneCount(body)))
		return
	}
	for _, block := range blocks {
		switch block.Type {
		case "tool_use":
			// The call itself is the session's own words: it chose the tool and wrote the arguments.
			// Both are kept so the result, which carries neither, can be read when it arrives.
			if block.ID != "" && block.Name != "" {
				calls[block.ID] = call{tool: block.Name, command: commandOf(block)}
			}
			into.Count(contextspend.Turns,
				int64(utf8.RuneCountInString(block.Name)+utf8.RuneCount(block.Input)))
		case "tool_result":
			asked := calls[block.ToolUseID]
			into.Count(contextspend.Of(asked.tool, asked.command), textOf(block.Content))
		case "text":
			into.Count(categoryOfRecord(record.Type), int64(utf8.RuneCountInString(block.Text)))
		case "thinking":
			into.Count(contextspend.Turns, int64(utf8.RuneCountInString(block.Thinking)))
		default:
			into.Count(categoryOfRecord(record.Type), int64(utf8.RuneCountInString(block.Text)))
		}
	}
}

// commandOf is what a call to the shell asked to run, and empty for every other tool and for
// arguments this cannot read.
func commandOf(block contentBlock) string {
	if block.Name != contextspend.Shell || len(block.Input) == 0 {
		return ""
	}
	var asked shellCall
	if json.Unmarshal(block.Input, &asked) != nil {
		return ""
	}
	return asked.Command
}

// categoryOfRecord says whose words a plain piece of text is. The session wrote what is on its own
// records; everything else reached it from outside.
func categoryOfRecord(kind string) contextspend.Category {
	if kind == assistantRecord {
		return contextspend.Turns
	}
	return contextspend.Told
}

// textOf is how long what a tool returned is. It comes back as a string on most tools and as a list
// of blocks on the ones that return more than text, so both are measured.
func textOf(returned json.RawMessage) int64 {
	if len(returned) == 0 {
		return 0
	}
	var text string
	if json.Unmarshal(returned, &text) == nil {
		return int64(utf8.RuneCountInString(text))
	}
	var blocks []contentBlock
	if json.Unmarshal(returned, &blocks) != nil {
		return int64(utf8.RuneCount(returned))
	}
	var characters int64
	for _, block := range blocks {
		characters += int64(utf8.RuneCountInString(block.Text))
	}
	return characters
}
