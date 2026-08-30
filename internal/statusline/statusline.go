// Package statusline draws the line the model runtime keeps under the conversation.
//
// An operator attached to a session is talking to the model directly, and the one number that
// decides whether the conversation is still worth continuing, how much of the context window it has
// filled, was nowhere on the screen. It is not in the console, it is not in the header, and asking
// for it costs a task. So the session says it itself, on every redraw, in the one place that is
// always in front of the person typing.
package statusline

import (
	"encoding/json"
	"fmt"

	"github.com/atlantic-blue/krewe/internal/display"
)

// Warn is the share of the context window at which the line stops being information and starts being
// a warning. Thirty rather than something closer to full: what the operator does about it, finishing
// the task, compacting, or opening a fresh session, takes a while and is much cheaper decided early
// than at ninety.
const Warn = 30

// Input is the part of the runtime's status line payload this reads. The payload carries the model,
// the workspace, what the session has cost and more; everything not named here is ignored, so a
// runtime that adds a field does not stop this from drawing.
type Input struct {
	Window *Window `json:"context_window"`
}

// Window is what the runtime says about the context window: how much of it the next task will carry,
// and how much there is. Used counts what was sent, including everything read back from the cache,
// because that is the context rather than the part of it charged as new.
type Window struct {
	Used int64 `json:"total_input_tokens"`
	Size int64 `json:"context_window_size"`
}

// Line reads one payload and returns the line to print. It never fails: a status line has one line
// to say anything in, and a command that exits with an error says nothing at all, which reads as the
// system being broken rather than as the runtime being older than this build.
func Line(payload []byte) string {
	var said Input
	if err := json.Unmarshal(payload, &said); err != nil {
		return "context: the model runtime said something this build cannot read"
	}
	if said.Window == nil || said.Window.Size <= 0 {
		return "context: this model runtime does not say how much is used"
	}
	return draw(said.Window.Used, said.Window.Size)
}

// draw writes the line for a window of a known size.
//
// The threshold is read off the percentage that is printed rather than off the exact share, so a
// line that says thirty per cent is never a line that declined to warn.
//
// The share itself is worked out where the console works it out, so the line under the prompt and the
// listing can never disagree about the same conversation. The count beside it stays what was
// reported, even where the share stops at a hundred.
func draw(used, size int64) string {
	if used < 0 {
		used = 0
	}
	percent := display.Share(used, size)
	line := fmt.Sprintf("context %d%% used (%s of %s)", percent, tokens(used), tokens(size))
	if percent < Warn {
		return line
	}
	return warning(fmt.Sprintf("%s, over the %d%% mark", line, Warn))
}

// tokens is a count as a person reads it, never blank: display leaves a zero count empty for a table
// column, and a gap in the middle of a sentence reads as a defect.
func tokens(count int64) string {
	if count <= 0 {
		return "0"
	}
	return display.Tokens(count)
}

// warning is the same line in bold yellow. Written as escape sequences rather than through the
// console's styling, because this is printed to a pipe rather than to a terminal: a styling library
// asked whether it is on a terminal answers no here and drops the colour, while the runtime reading
// the pipe renders whatever escapes it is handed.
//
// The words carry the warning on their own. A reader whose terminal eats the colour, or who cannot
// tell yellow from the rest of the line, still reads what it says.
func warning(line string) string {
	return "\033[1;33m" + line + "\033[0m"
}

// WindowSize is how big the runtime says the context window is, and whether it said. Zero where the
// payload carries no window, which is a runtime older than this build rather than a window of no
// size.
func WindowSize(payload []byte) (int64, bool) {
	var said Input
	if err := json.Unmarshal(payload, &said); err != nil {
		return 0, false
	}
	if said.Window == nil || said.Window.Size <= 0 {
		return 0, false
	}
	return said.Window.Size, true
}
