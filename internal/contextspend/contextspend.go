// Package contextspend says where a session's context went.
//
// Context is the budget that decides how good the work is. The system already says how full a
// window is, and a share on its own moves nothing: a session at eighty per cent filled up on the
// code it had to read, on tool output it read once, or on its own repeated attempts, and until
// somebody can say which, no change to how a session reads can be argued for or against.
//
// So the conversation is read back and every character of it is put in one of four places. The
// categories are the three the work is spent on, plus what a person and the system typed in.
//
// The count is characters and not tokens, for the same reason the level sizes are characters: the
// transcript holds text, every model counts tokens its own way, and a token count worked out here
// would be a made up number sitting beside a real one. Tokens arrive in Check below, where the
// model's own count is the thing being compared against rather than the thing being guessed.
package contextspend

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// A Category is one place a session's context went.
type Category string

const (
	// Reads is the contents of files, however the session opened them: with a reading tool, or with a
	// reading command in the shell. The published claim this exists to test is that reading a symbol
	// costs a fraction of reading the file it lives in, so this is the category that claim is about.
	Reads Category = "reads"
	// Tools is what every other tool returned: a search, a page fetched, the answer of a sub agent,
	// and every shell command that was not printing a file.
	Tools Category = "tools"
	// Turns is the session's own words: what it wrote, what it thought, and the calls it made. It is
	// the category a session controls directly, so a session going in circles shows up here.
	Turns Category = "turns"
	// Told is what reached the session from outside a tool: the exec it was given, the answers to its
	// questions, and whatever the runtime put in front of it. Anything the reader does not recognise
	// lands here as well, so no character of the conversation is dropped on its way into a total.
	Told Category = "told"
)

// Categories are the four, in the order a report prints them, which is largest first on a normal
// conversation rather than alphabetical.
func Categories() []Category { return []Category{Reads, Tools, Turns, Told} }

// Meaning is the half sentence a report prints beside a category, because the word on its own does
// not say what was counted.
func (c Category) Meaning() string {
	switch c {
	case Reads:
		return "files it read"
	case Tools:
		return "what every other tool returned"
	case Turns:
		return "its own words, thinking and calls"
	case Told:
		return "what it was told"
	default:
		return ""
	}
}

// A Spend is how many characters of a conversation went to each category.
//
// The four fields are the whole of it. Total is their sum by construction, so a caller can never be
// shown a breakdown whose parts do not add up to the number above them.
type Spend struct {
	Reads int64
	Tools int64
	Turns int64
	Told  int64
}

// Count adds characters to one category. An unknown category is counted as told rather than dropped,
// which is the same rule the reader follows for a record it does not recognise.
func (s *Spend) Count(category Category, characters int64) {
	if characters <= 0 {
		return
	}
	switch category {
	case Reads:
		s.Reads += characters
	case Tools:
		s.Tools += characters
	case Turns:
		s.Turns += characters
	default:
		s.Told += characters
	}
}

// Add sums two, so a caller can total the sessions a job used without knowing what is in one.
func (s Spend) Add(other Spend) Spend {
	return Spend{
		Reads: s.Reads + other.Reads,
		Tools: s.Tools + other.Tools,
		Turns: s.Turns + other.Turns,
		Told:  s.Told + other.Told,
	}
}

// Of is one category's characters.
func (s Spend) Of(category Category) int64 {
	switch category {
	case Reads:
		return s.Reads
	case Tools:
		return s.Tools
	case Turns:
		return s.Turns
	case Told:
		return s.Told
	default:
		return 0
	}
}

// Total is every character this accounting saw. It is the sum of the four and nothing else.
func (s Spend) Total() int64 { return s.Reads + s.Tools + s.Turns + s.Told }

// Empty says nothing was measured, which is a conversation nobody has had rather than one that
// spent nothing.
func (s Spend) Empty() bool { return s == Spend{} }

// Share is what part of the whole one category took, as a whole number out of a hundred. Rounded to
// nearest, so four categories can print to a hundred and one and that is arithmetic rather than a
// defect.
func (s Spend) Share(category Category) int64 {
	total := s.Total()
	if total <= 0 {
		return 0
	}
	return (s.Of(category)*100 + total/2) / total
}

// Largest is the category that took the most, which is the answer the whole measurement exists to
// give. Ties go to the earlier category in Categories, so the same conversation always answers the
// same way.
func (s Spend) Largest() Category {
	largest, most := Told, int64(-1)
	for _, category := range Categories() {
		if got := s.Of(category); got > most {
			largest, most = category, got
		}
	}
	return largest
}

// Cell is the breakdown in one column of a listing: the category that dominates and by how much. A
// conversation nobody has spoken in says nothing rather than naming a category out of four zeroes.
func (s Spend) Cell() string {
	if s.Empty() {
		return ""
	}
	largest := s.Largest()
	return string(largest) + " " + strconv.FormatInt(s.Share(largest), 10) + "%"
}

// Lines is the breakdown as a person reads it, one category per line, largest first.
func (s Spend) Lines() []string {
	if s.Empty() {
		return nil
	}
	ordered := Categories()
	// A short list, so the sort is written out rather than pulled in. Largest first, and the order in
	// Categories breaks a tie, so two runs over one conversation print the same list.
	for i := 1; i < len(ordered); i++ {
		for j := i; j > 0 && s.Of(ordered[j]) > s.Of(ordered[j-1]); j-- {
			ordered[j], ordered[j-1] = ordered[j-1], ordered[j]
		}
	}
	lines := make([]string, 0, len(ordered))
	for _, category := range ordered {
		lines = append(lines, fmt.Sprintf("%-6s %3d%%  %s characters, %s",
			category, s.Share(category), commas(s.Of(category)), category.Meaning()))
	}
	return lines
}

// The measured ratio of characters to tokens in a session's conversation, held as a fraction because
// the figure is not a whole number.
//
// It is 1.9 characters a token, and it is a measurement rather than a rule of thumb. The usual figure
// is four characters a token, which is prose. A session's conversation is not prose: it is code,
// paths, identifiers, JSON and terminal output, and all of those cost more tokens for the same
// number of characters. Taking four here would have reported every breakdown as covering half of what
// it covers.
//
// Measured on 31 August 2026 over 101 conversations held in this crew's own sandbox, by the only
// method that needs no assumption: between one answer and the next, the transcript grew by so many
// characters and the model's own count grew by so many tokens. 42,529,463 characters against
// 22,587,622 tokens is 1.883. docs/CONTEXT-SPEND.md holds the working and the command that repeats it.
//
// It describes this crew's traffic, not the model. Different work reads differently, so the number
// is worth measuring again rather than inheriting.
const (
	measuredCharacters = 19
	measuredTokens     = 10
)

// Tokens is what a character count comes to in the model's own units, at the measured ratio.
func Tokens(characters int64) int64 { return characters * measuredTokens / measuredCharacters }

// A Check is the breakdown held against what the model itself counted.
//
// It exists because a breakdown whose parts do not add up to the model's own total is a number that
// will be trusted and is wrong. The parts always add up to Total, which is arithmetic and proves
// nothing on its own. The question that decides whether the shares mean anything is whether Total is
// a measurement of the same context the model says it is carrying.
//
// Counted is the model's own figure for what the last answer carried, which is the same number the
// session listing shows as used.
type Check struct {
	// Measured is the breakdown in tokens, at the measured ratio.
	Measured int64
	// Counted is the model's own count of the context it carries, in tokens.
	Counted int64
}

// Against holds a breakdown against the model's own count.
func (s Spend) Against(counted int64) Check {
	return Check{Measured: Tokens(s.Total()), Counted: counted}
}

// Known says the model has counted anything at all. Nothing counted means the comparison cannot be
// made, and a share worked out from a zero is a claim rather than a check.
func (c Check) Known() bool { return c.Counted > 0 && c.Measured > 0 }

// Share is what part of the model's own count this breakdown accounts for, out of a hundred. It is
// not capped: over a hundred is a real answer and it says the accounting has counted something the
// model did not charge for, which is a defect worth seeing rather than a number worth hiding.
func (c Check) Share() int64 {
	if !c.Known() {
		return 0
	}
	return (c.Measured*100 + c.Counted/2) / c.Counted
}

// Line is the comparison as a person reads it, and it names the part the breakdown does not explain
// rather than leaving a reader to decide the accounting is broken.
//
// The transcript holds the conversation and nothing else. The system prompt, the definitions of every
// tool the session holds, and whatever the runtime puts around them are all in the model's count and
// in no record on this machine. So a breakdown that accounted for the whole of it would be the
// suspicious answer, and the share is smallest on the shortest conversations, where that floor is
// most of the window.
func (c Check) Line() string {
	if !c.Known() {
		return "the model has counted nothing into this context yet, so there is nothing to check the breakdown against"
	}
	return fmt.Sprintf("about %s tokens of the %s the model says it carries: %d%%. "+
		"The rest is the system prompt and the tool definitions, which no transcript holds.",
		commas(c.Measured), commas(c.Counted), c.Share())
}

// readsFiles are the tools that exist to hand back the contents of a file. A search returns the lines
// that matched rather than the file, so it is not one of these: a search is the cheap read the claim
// under test is about, and counting it here would put the cheap thing and the expensive thing in one
// number.
var readsFiles = map[string]bool{
	"Read":         true,
	"NotebookRead": true,
}

// Shell is the tool a session runs commands with.
const Shell = "Bash"

// shellReads are the commands that print a file, matched on the first word of the command and
// nothing cleverer.
//
// The list is short on purpose. It is here because a session told to work through the shell reads
// its files with `cat` and `sed -n`, and without this rule those characters land in tool output and
// the reads share reads two per cent when the measured answer is nearer thirty. A number that wrong
// is worse than no number.
var shellReads = map[string]bool{
	"cat": true, "head": true, "tail": true, "less": true, "more": true, "sed": true,
}

// Of says which category what a tool returned belongs to. The command is what the session asked the
// shell to run, and empty for every other tool.
//
// A result whose call this never saw is tool output. The alternative is to drop it, and dropping it
// would make every other share larger than it is.
func Of(tool, command string) Category {
	if readsFiles[tool] {
		return Reads
	}
	if tool == Shell && ReadsAFile(command) {
		return Reads
	}
	return Tools
}

// ReadsAFile says whether a shell command prints a file.
//
// It reads the first word and stops. `cat internal/job/controller.go` is a read; `catalogue --help`
// is not, so the word is matched whole rather than as a prefix. `sed` has to be printing rather than
// editing, which is what `-n` says.
//
// It is a floor and it says so here. A read hidden behind a `cd` or an `awk`, or one the session
// opened inside a script, is counted as tool output, so the reads share this produces is the least
// the sessions read rather than the most.
func ReadsAFile(command string) bool {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return false
	}
	word := fields[0]
	if cut := strings.LastIndex(word, "/"); cut >= 0 {
		word = word[cut+1:]
	}
	if !shellReads[word] {
		return false
	}
	if word == "sed" {
		return slices.Contains(fields, "-n")
	}
	return true
}

// commas writes a number the way a person reads one.
func commas(n int64) string {
	digits := strconv.FormatInt(n, 10)
	sign := ""
	if strings.HasPrefix(digits, "-") {
		sign, digits = "-", digits[1:]
	}
	var out strings.Builder
	for i, digit := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			out.WriteByte(',')
		}
		out.WriteRune(digit)
	}
	return sign + out.String()
}
